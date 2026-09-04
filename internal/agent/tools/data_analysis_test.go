package tools

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestTableNameIsIsolatedBySession(t *testing.T) {
	knowledge := &types.Knowledge{ID: "knowledge-1"}
	first := &DataAnalysisTool{sessionID: "session-a"}
	second := &DataAnalysisTool{sessionID: "session-b"}
	if first.TableName(knowledge) == second.TableName(knowledge) {
		t.Fatal("table names must be isolated by analysis session")
	}
	if got := first.TableName(knowledge); !strings.HasPrefix(got, "k_knowledge_1_") || len(got) != len("k_knowledge_1_")+12 {
		t.Fatalf("unexpected bounded table name: %s", got)
	}
}

func TestTableNameIsIsolatedByKnowledgeVersion(t *testing.T) {
	first := &types.Knowledge{ID: "knowledge-1", CurrentVersionID: "version-a"}
	second := &types.Knowledge{ID: "knowledge-1", CurrentVersionID: "version-b"}
	tool := &DataAnalysisTool{sessionID: "session-a"}
	if tool.TableName(first) == tool.TableName(second) {
		t.Fatal("table names must be isolated by knowledge version")
	}
}

func TestDataAnalysisAuthorizationDistinguishesAgentAndInternalCallers(t *testing.T) {
	agent := AgentDataAnalysisAuthorization(types.SearchTargets{}, nil)
	internal := InternalDataAnalysisAuthorization()
	if agent.mode != dataAnalysisAuthorizationAgentScope {
		t.Fatalf("unexpected agent authorization mode: %v", agent.mode)
	}
	if internal.mode != dataAnalysisAuthorizationInternal {
		t.Fatalf("unexpected internal authorization mode: %v", internal.mode)
	}
}

func TestValidateDataAnalysisSQLAllowsSafeAggregate(t *testing.T) {
	if err := validateDataAnalysisSQL(`SELECT COUNT(*) FROM "people" WHERE 学历 = '硕士'`, "people"); err != nil {
		t.Fatalf("expected safe aggregate to pass validation: %v", err)
	}
}

func TestValidateDataAnalysisSQLBlocksDuckDBExternalIO(t *testing.T) {
	if err := validateDataAnalysisSQL(`SELECT read_text('/etc/passwd') FROM "people"`, "people"); err == nil {
		t.Fatal("expected DuckDB external I/O function to be rejected")
	}
}

func TestFormatQueryResultsKeepsResultsWithoutSQL(t *testing.T) {
	tool := &DataAnalysisTool{}
	output := tool.formatQueryResults([]map[string]string{{"master_count": "41"}})

	if strings.Contains(output, "SELECT") || strings.Contains(output, "DuckDB") || strings.Contains(output, "internal_query") {
		t.Fatalf("answer evidence must not expose query implementation details: %q", output)
	}
	if !strings.Contains(output, "Returned 1 rows") || !strings.Contains(output, `"master_count":"41"`) {
		t.Fatalf("answer evidence must retain query results: %q", output)
	}
}

func TestTableNameFitsSQLParserIdentifierLimit(t *testing.T) {
	tool := &DataAnalysisTool{sessionID: "40bc6638-0128-4952-a36f-a670e94a7902"}
	knowledge := &types.Knowledge{
		ID:               "b1e8124d-fd1a-4093-9907-da77757d1934",
		CurrentVersionID: "e44a6104-2afa-4c22-8888-123456789abc",
	}
	if got := tool.TableName(knowledge); len(got) > maxSQLIdentifierLength {
		t.Fatalf("table name exceeds SQL parser limit: %d characters (%s)", len(got), got)
	}
}

func TestReconcileSQLTableUsesAuthorizedSessionTable(t *testing.T) {
	schema := &TableSchema{TableName: "k_authorized_123"}
	got := reconcileSQLTableWithSchema(`SELECT * FROM "k_hallucinated-123" JOIN other_table ON 1=1`, schema)
	want := `SELECT * FROM "k_authorized_123" JOIN "k_authorized_123" ON 1=1`
	if got != want {
		t.Fatalf("unexpected reconciled SQL: %s", got)
	}
}

func TestBuildExcelCreateTableSQL_NoSheets(t *testing.T) {
	got := buildExcelCreateTableSQL("tbl", "/tmp/data.xlsx", nil)
	want := `CREATE TABLE "tbl" AS SELECT * FROM read_xlsx('/tmp/data.xlsx', header=true, all_varchar=true)`
	if got != want {
		t.Fatalf("mismatch.\n got: %s\nwant: %s", got, want)
	}
}

func TestBuildExcelCreateTableSQL_SingleSheetTagsSource(t *testing.T) {
	got := buildExcelCreateTableSQL("tbl", "/tmp/data.xlsx", []string{"Sheet1"})

	// Must use read_xlsx (excel extension) with explicit sheet param.
	if !strings.Contains(got, "FROM read_xlsx('/tmp/data.xlsx', sheet = 'Sheet1', header=true, all_varchar=true)") {
		t.Fatalf("expected read_xlsx with sheet param, got: %s", got)
	}
	// Must tag the source sheet name via the synthetic column so downstream
	// SQL behaves consistently between single- and multi-sheet workbooks.
	if !strings.Contains(got, "'Sheet1' AS "+excelSheetNameColumn) {
		t.Fatalf("expected sheet-name column, got: %s", got)
	}
}

func TestBuildExcelCreateTableSQL_MultiSheetUsesUnionAllByName(t *testing.T) {
	got := buildExcelCreateTableSQL("tbl", "/tmp/data.xlsx", []string{"Sheet1", "Sheet2", "报表"})

	// Each sheet must appear as a SELECT reading that specific sheet, and
	// the __sheet_name column must carry its name for per-sheet filtering.
	for _, sheet := range []string{"Sheet1", "Sheet2", "报表"} {
		needleRead := "FROM read_xlsx('/tmp/data.xlsx', sheet = '" + sheet + "', header=true, all_varchar=true)"
		needleTag := "'" + sheet + "' AS " + excelSheetNameColumn
		if !strings.Contains(got, needleRead) {
			t.Fatalf("missing read_xlsx for sheet %q in:\n%s", sheet, got)
		}
		if !strings.Contains(got, needleTag) {
			t.Fatalf("missing __sheet_name tag for sheet %q in:\n%s", sheet, got)
		}
	}

	// Must combine with UNION ALL BY NAME so schema drift between sheets is
	// tolerated.
	if !strings.Contains(got, "UNION ALL BY NAME") {
		t.Fatalf("expected UNION ALL BY NAME in multi-sheet SQL, got:\n%s", got)
	}

	// Exactly N-1 UNIONs for N sheets.
	if strings.Count(got, "UNION ALL BY NAME") != 2 {
		t.Fatalf("expected 2 UNION ALL BY NAME separators, got %d in:\n%s",
			strings.Count(got, "UNION ALL BY NAME"), got)
	}
}

func TestBuildExcelCreateTableSQL_EscapesSingleQuotes(t *testing.T) {
	// Sheet name and file path both contain single quotes, which must be
	// doubled to produce a valid SQL literal.
	sheets := []string{"Jo's data"}
	got := buildExcelCreateTableSQL("tbl", "/tmp/O'Brien/data.xlsx", sheets)

	if !strings.Contains(got, "sheet = 'Jo''s data'") {
		t.Fatalf("sheet name was not escaped, got:\n%s", got)
	}
	if !strings.Contains(got, "read_xlsx('/tmp/O''Brien/data.xlsx'") {
		t.Fatalf("file path was not escaped, got:\n%s", got)
	}
	if !strings.Contains(got, "'Jo''s data' AS "+excelSheetNameColumn) {
		t.Fatalf("sheet-name literal was not escaped, got:\n%s", got)
	}
}

func TestSqlSingleQuoteEscape(t *testing.T) {
	cases := map[string]string{
		"":               "",
		"no_quote":       "no_quote",
		"a'b":            "a''b",
		"''":             "''''",
		"mix'ed'quote":   "mix''ed''quote",
		"中文 with 'quote": "中文 with ''quote",
	}
	for in, want := range cases {
		if got := sqlSingleQuoteEscape(in); got != want {
			t.Errorf("sqlSingleQuoteEscape(%q) = %q, want %q", in, got, want)
		}
	}
}
