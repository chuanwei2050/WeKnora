package tools

import (
	"context"
	"testing"
)

func TestMultilineProfileComesFromSourceValues(t *testing.T) {
	db := regressionDuckDB(t)
	if _, err := db.Exec(`CREATE TABLE varied AS SELECT 'A' || chr(10) || 'B' AS arbitrary_field, 'plain' AS other`); err != nil {
		t.Fatal(err)
	}
	schema, err := (&DataAnalysisTool{db: db}).profileAnalysisTable(context.Background(), "varied")
	if err != nil {
		t.Fatal(err)
	}
	if !schema.Columns[0].Multiline || schema.Columns[1].Multiline {
		t.Fatalf("incorrect source profile: %+v", schema.Columns)
	}
}

func TestProfileDoesNotInferWholeColumnTypeFromBoundedSample(t *testing.T) {
	db := regressionDuckDB(t)
	if _, err := db.Exec(`CREATE TABLE sampled(value VARCHAR); INSERT INTO sampled SELECT CAST(i AS VARCHAR) FROM range(10000) AS t(i); INSERT INTO sampled VALUES ('not-a-number')`); err != nil {
		t.Fatal(err)
	}
	schema, err := (&DataAnalysisTool{db: db}).profileAnalysisTable(context.Background(), "sampled")
	if err != nil {
		t.Fatal(err)
	}
	if got := schema.Columns[0].AnalysisType; got != "" {
		t.Fatalf("bounded sample was treated as proof for the whole column: %q", got)
	}
}

func TestProfileScansLargeTextColumnsForMultilineValues(t *testing.T) {
	db := regressionDuckDB(t)
	if _, err := db.Exec(`CREATE TABLE sampled_lines(value VARCHAR, plain VARCHAR); INSERT INTO sampled_lines SELECT 'plain', 'single' FROM range(10000); INSERT INTO sampled_lines VALUES ('A' || chr(10) || 'B', 'single')`); err != nil {
		t.Fatal(err)
	}
	schema, err := (&DataAnalysisTool{db: db}).profileAnalysisTable(context.Background(), "sampled_lines")
	if err != nil {
		t.Fatal(err)
	}
	if !schema.Columns[0].Multiline || schema.Columns[1].Multiline {
		t.Fatalf("large-column multiline detection was inaccurate: %+v", schema.Columns)
	}
}
