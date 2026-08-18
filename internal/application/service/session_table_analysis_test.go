package service

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/types"
)

func TestTableAnalysisPlanningPromptUsesLoadedTableName(t *testing.T) {
	prompt := tableAnalysisPlanningPrompt(
		"arbitrary-knowledge-id",
		[]types.TableAnalysisQuery{{ID: "q1", Query: "任意业务问题"}},
		&tools.TableSchema{TableName: "k_runtime_table_42"},
	)
	if !strings.Contains(prompt, "Use table_schema.table_name exactly as the only table name") {
		t.Fatalf("expected prompt to require the runtime table name, got %q", prompt)
	}
	if !strings.Contains(prompt, `"table_name":"k_runtime_table_42"`) {
		t.Fatalf("expected prompt to include the loaded table name, got %q", prompt)
	}
	if !strings.Contains(prompt, "knowledge_id is authorization context") {
		t.Fatalf("expected prompt to distinguish authorization context from the SQL table")
	}
}

func TestBoundTableAnalysisSQLAddsServerLimit(t *testing.T) {
	query, err := boundTableAnalysisSQL(`SELECT "name" FROM knowledge-id WHERE "status" = 'active'`, 20)
	if err != nil {
		t.Fatalf("boundTableAnalysisSQL returned an error: %v", err)
	}
	if !strings.HasPrefix(query, `SELECT "name" FROM knowledge-id`) {
		t.Fatalf("expected the original allowed table to remain top-level, got %q", query)
	}
	if !strings.HasSuffix(query, "LIMIT 20") {
		t.Fatalf("expected the server row limit, got %q", query)
	}
}

func TestBoundTableAnalysisSQLReplacesTrailingModelLimit(t *testing.T) {
	query, err := boundTableAnalysisSQL(`SELECT * FROM arbitrary_table LIMIT 500 OFFSET 2`, 7)
	if err != nil {
		t.Fatalf("boundTableAnalysisSQL returned an error: %v", err)
	}
	if query != `SELECT * FROM arbitrary_table LIMIT 7` {
		t.Fatalf("expected the trusted server limit to replace the model limit, got %q", query)
	}
}

func TestBoundTableAnalysisSQLRejectsNonSelectAndInvalidLimits(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		maxRows int
	}{
		{name: "update", query: "UPDATE knowledge-id SET status = 'inactive'", maxRows: 20},
		{name: "delete", query: "DELETE FROM knowledge-id", maxRows: 20},
		{name: "zero limit", query: "SELECT * FROM knowledge-id", maxRows: 0},
		{name: "excessive limit", query: "SELECT * FROM knowledge-id", maxRows: 51},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := boundTableAnalysisSQL(test.query, test.maxRows); err == nil {
				t.Fatalf("expected query to be rejected: %q", test.query)
			}
		})
	}
}
