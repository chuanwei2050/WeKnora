package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestReviewMixedTextCanBeOrderedAsText(t *testing.T) {
	db := regressionDuckDB(t)
	if _, err := db.Exec(`CREATE TABLE codes(code VARCHAR); INSERT INTO codes VALUES ('A'),('10'),('1')`); err != nil {
		t.Fatal(err)
	}
	tool := &DataAnalysisTool{db: db}
	schema, err := tool.profileAnalysisTable(context.Background(), "codes")
	if err != nil {
		t.Fatal(err)
	}
	tool.loadedSchemas = map[string]*TableSchema{"doc": schema}
	payload, _ := json.Marshal(DataAnalysisInput{KnowledgeID: "doc", Sql: `SELECT code FROM data ORDER BY code`})
	result, err := tool.Execute(context.Background(), payload)
	if err != nil {
		t.Fatalf("text ordering must not require a numeric conversion: %v", err)
	}
	rows := result.Data["rows"].([]map[string]string)
	if len(rows) != 3 || rows[0]["code"] != "1" || rows[1]["code"] != "10" || rows[2]["code"] != "A" {
		t.Fatalf("unexpected text ordering: %v", rows)
	}
}
