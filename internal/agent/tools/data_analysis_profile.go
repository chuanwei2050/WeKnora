package tools

import (
	"context"
	"fmt"
	"strings"
)

const dataAnalysisProfileSampleRows = 10000

func analysisProfileSource(table string) string {
	return fmt.Sprintf("(SELECT * FROM %s LIMIT %d) AS profile_sample", quoteDuckDBIdentifier(table), dataAnalysisProfileSampleRows)
}

// Profile the complete column without changing stored values. SQL must choose
// conversions explicitly; column names cannot establish numeric semantics.
func (t *DataAnalysisTool) profileAnalysisTable(ctx context.Context, table string) (*TableSchema, error) {
	schema, err := t.LoadFromTable(ctx, table)
	if err != nil {
		return nil, err
	}
	expressions := make([]string, 0, len(schema.Columns)*6)
	for _, column := range schema.Columns {
		value := "NULLIF(trim(CAST(" + quoteDuckDBIdentifier(column.Name) + " AS VARCHAR)), '')"
		expressions = append(expressions, "count("+value+")", "count(try_cast("+value+" AS DECIMAL(38,10))) FILTER (WHERE regexp_matches("+value+", '^[+-]?[0-9]+([.][0-9]{1,10})?$'))", "count(try_cast("+value+" AS DATE)) FILTER (WHERE regexp_matches("+value+", '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'))", "count(*) FILTER (WHERE regexp_matches("+value+", '^[-+]?0[0-9]+'))", "count(*) FILTER (WHERE regexp_matches("+value+", '^[+-]?[0-9]+([.][0-9]+)?$'))", "count(*) FILTER (WHERE contains("+value+", chr(10)) OR contains("+value+", chr(13)))")
	}
	if len(expressions) == 0 {
		return schema, nil
	}
	counts := make([]int64, len(expressions))
	dest := make([]interface{}, len(counts))
	for i := range counts {
		dest[i] = &counts[i]
	}
	if err := t.db.QueryRowContext(ctx, "SELECT "+strings.Join(expressions, ",")+" FROM "+analysisProfileSource(table)).Scan(dest...); err != nil {
		return nil, err
	}
	if schema.RowCount > dataAnalysisProfileSampleRows {
		fullExpressions := make([]string, 0, len(schema.Columns))
		fullIndexes := make([]int, 0, len(schema.Columns))
		for i, column := range schema.Columns {
			if column.Type != "VARCHAR" || counts[i*6+5] > 0 {
				continue
			}
			value := "CAST(" + quoteDuckDBIdentifier(column.Name) + " AS VARCHAR)"
			fullExpressions = append(fullExpressions, "count(*) FILTER (WHERE contains("+value+", chr(10)) OR contains("+value+", chr(13)))")
			fullIndexes = append(fullIndexes, i)
		}
		if len(fullExpressions) > 0 {
			fullCounts := make([]int64, len(fullExpressions))
			fullDest := make([]interface{}, len(fullCounts))
			for i := range fullCounts {
				fullDest[i] = &fullCounts[i]
			}
			if err := t.db.QueryRowContext(ctx, "SELECT "+strings.Join(fullExpressions, ",")+" FROM "+quoteDuckDBIdentifier(table)).Scan(fullDest...); err != nil {
				return nil, err
			}
			for i, count := range fullCounts {
				if count > 0 {
					counts[fullIndexes[i]*6+5] = count
				}
			}
		}
	}
	for i := range schema.Columns {
		column := &schema.Columns[i]
		n, numeric, dates, leadingZero, numericText := counts[i*6], counts[i*6+1], counts[i*6+2], counts[i*6+3], counts[i*6+4]
		column.Multiline = counts[i*6+5] > 0
		if column.Type != "VARCHAR" {
			continue
		}
		if n == 0 {
			column.AnalysisType = "empty column"
			continue
		}
		if leadingZero > 0 {
			column.AnalysisType = "text with leading zeros; preserve exact source values"
			continue
		}
		// A bounded sample can prove that a risky value exists, but cannot prove
		// that every value in a larger column has the same format.
		if schema.RowCount > dataAnalysisProfileSampleRows {
			if (numericText > 0 && numericText < n) || (dates > 0 && dates < n) {
				column.AnalysisType = "mixed formats observed in a bounded sample; do not silently discard failed conversions; clarify the data before numeric/date aggregation"
			}
			continue
		}
		if numeric == n {
			column.AnalysisType = "numeric text; use explicit CAST to DECIMAL(38,10) for numeric aggregates, comparisons and ordering"
		} else if numericText == n {
			column.AnalysisType = "numeric text; choose an explicit DECIMAL precision and scale that preserves every source value"
		} else if dates == n {
			column.AnalysisType = "date/time text; use explicit CAST to TIMESTAMP for chronological operations"
		} else if numericText > 0 || dates > 0 {
			column.AnalysisType = "mixed formats; do not silently discard failed conversions; clarify the data before numeric/date aggregation"
		}

	}
	if err := t.profileAnalysisValues(ctx, schema); err != nil {
		return nil, err
	}
	return schema, nil
}

// Ground SQL generation in stored terminology across columns, independently of
// retrieval top-k. Fragments expose compound/multiline values without asking
// the model to infer canonical categories from column names alone.
func (t *DataAnalysisTool) profileAnalysisValues(ctx context.Context, schema *TableSchema) error {
	queries := make([]string, 0, len(schema.Columns))
	for i := range schema.Columns {
		column := &schema.Columns[i]
		column.ValueExamples = nil
		if column.Type != "VARCHAR" {
			continue
		}
		queries = append(queries, fmt.Sprintf("(SELECT %d AS column_index, fragment FROM (SELECT trim(unnest(string_split_regex(%s, '[\\r\\n]+'))) AS fragment FROM %s) WHERE fragment <> '' GROUP BY fragment ORDER BY count(*) DESC, fragment LIMIT 12)", i, quoteDuckDBIdentifier(column.Name), analysisProfileSource(schema.TableName)))
	}
	if len(queries) == 0 {
		return nil
	}
	rows, err := t.db.QueryContext(ctx, strings.Join(queries, " UNION ALL "))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var index int
		var value string
		if err := rows.Scan(&index, &value); err != nil {
			return err
		}
		runes := []rune(value)
		if len(runes) > 120 {
			value = string(runes[:120]) + "…"
		}
		schema.Columns[index].ValueExamples = append(schema.Columns[index].ValueExamples, value)
	}
	return rows.Err()
}
