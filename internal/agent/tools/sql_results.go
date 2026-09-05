package tools

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
)

func scanSQLRows(rows *sql.Rows, maxRows, maxBytes int) ([]string, []map[string]interface{}, bool, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, nil, false, err
	}
	seen := make(map[string]bool, len(columns))
	for _, column := range columns {
		if seen[column] {
			return nil, nil, false, fmt.Errorf("duplicate result column %q; use distinct SQL aliases", column)
		}
		seen[column] = true
	}
	results := make([]map[string]interface{}, 0)
	bytesUsed := 0
	truncated := false
	values := make([]interface{}, len(columns))
	pointers := make([]interface{}, len(columns))
	for i := range values {
		pointers[i] = &values[i]
	}
	for rows.Next() {
		if len(results) >= maxRows {
			truncated = true
			break
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, nil, false, err
		}
		row := make(map[string]interface{}, len(columns))
		for i, column := range columns {
			value := values[i]
			if b, ok := value.([]byte); ok {
				value = string(b)
			}
			// JSON has no representation for non-finite SQL floating-point values.
			if number, ok := value.(float64); ok && (math.IsNaN(number) || math.IsInf(number, 0)) {
				value = fmt.Sprint(number)
			}
			row[column] = value
		}
		encoded, err := json.Marshal(row)
		if err != nil {
			return nil, nil, false, err
		}
		if bytesUsed+len(encoded) > maxBytes {
			truncated = true
			break
		}
		bytesUsed += len(encoded)
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, false, err
	}
	return columns, results, truncated, nil
}
