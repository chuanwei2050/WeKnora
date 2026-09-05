package tools

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func regressionDuckDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSQLIsolationKeepsEveryAliasAndOuterJoinSemantics(t *testing.T) {
	db := regressionDuckDB(t)
	_, err := db.Exec(`CREATE TABLE knowledges(id VARCHAR,knowledge_base_id VARCHAR,tenant_id INTEGER,deleted_at TIMESTAMP,title VARCHAR);
 CREATE TABLE knowledge_bases(id VARCHAR,tenant_id INTEGER);
 INSERT INTO knowledges VALUES ('own','allowed',1,NULL,'own data'),('foreign','forbidden',2,NULL,'other tenant data');
 INSERT INTO knowledge_bases VALUES ('allowed',1),('empty',1)`)
	if err != nil {
		t.Fatal(err)
	}
	secured, _, err := utils.ValidateAndSecureSQL(`SELECT a.title FROM knowledges a JOIN knowledges b ON a.id<>b.id`, utils.WithSecurityDefaults(1), utils.WithSoftDeleteFilter("knowledges"), utils.WithSearchScopeFilter([]string{"allowed"}, []string{"own"}))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(secured)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Fatal("self join bypassed tenant/scope isolation")
	}
	rows.Close()
	secured, _, err = utils.ValidateAndSecureSQL(`SELECT kb.id, COUNT(k.id) FROM knowledge_bases kb LEFT JOIN knowledges k ON kb.id=k.knowledge_base_id GROUP BY kb.id ORDER BY kb.id`, utils.WithSecurityDefaults(1))
	if err != nil {
		t.Fatal(err)
	}
	rows, err = db.Query(secured)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var id string
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			t.Fatal(err)
		}
		counts[id] = count
	}
	if len(counts) != 2 || counts["allowed"] != 1 || counts["empty"] != 0 {
		t.Fatalf("outer join changed: %+v", counts)
	}
}

func TestSQLBindingPreservesLiteralValues(t *testing.T) {
	db := regressionDuckDB(t)
	if _, err := db.Exec(`CREATE TABLE physical(note VARCHAR, "ab" VARCHAR); INSERT INTO physical VALUES ('from sales WHERE ORDER BY LIMIT','"a b"')`); err != nil {
		t.Fatal(err)
	}
	query, _, err := bindAnalysisSQL(`SELECT note FROM data WHERE note = 'from sales WHERE ORDER BY LIMIT' AND "a b" = '"a b"'`, &TableSchema{TableName: "physical", Columns: []ColumnInfo{{Name: "ab"}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	var note string
	if err := db.QueryRow(query).Scan(&note); err != nil {
		t.Fatal(err)
	}
	if note != "from sales WHERE ORDER BY LIMIT" {
		t.Fatal(note)
	}
	for _, query := range []string{`SELECT * FROM data LIMIT (SELECT count(*) FROM secret)`, `SELECT * FROM data OFFSET (SELECT count(*) FROM secret)`} {
		if err := validateDataAnalysisSQL(query, "data"); err == nil {
			t.Fatalf("unvalidated subquery accepted: %s", query)
		}
	}
}

func TestAnalysisInstanceOwnsOnlySuccessfullyCreatedTables(t *testing.T) {
	db := regressionDuckDB(t)
	ctx := context.Background()
	a := NewDataAnalysisTool(nil, nil, nil, nil, db, "same", InternalDataAnalysisAuthorization())
	b := NewDataAnalysisTool(nil, nil, nil, nil, db, "same", InternalDataAnalysisAuthorization())
	k := &types.Knowledge{ID: "doc"}
	name := a.TableName(k)
	if name == b.TableName(k) {
		t.Fatal("instances share table names")
	}
	path := filepath.Join(t.TempDir(), "data.csv")
	if err := os.WriteFile(path, []byte("value\n10\n20\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.LoadFromCSV(ctx, path, name); err != nil {
		t.Fatal(err)
	}
	if _, err := b.LoadFromCSV(ctx, path, name); err == nil {
		t.Fatal("duplicate create succeeded")
	}
	b.Cleanup(ctx)
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM "` + name + `"`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("another instance dropped owner table: %v", err)
	}
	a.Cleanup(ctx)
}

func TestAnalysisProfilesNumericColumnsWithoutDestroyingIdentifiers(t *testing.T) {
	db := regressionDuckDB(t)
	path := filepath.Join(t.TempDir(), "data.csv")
	if err := os.WriteFile(path, []byte("amount,employee_id,value,precise_price\n9,001,9,0.12345678901\n100,002,100,1.12345678901\n"), 0600); err != nil {
		t.Fatal(err)
	}
	tool := &DataAnalysisTool{db: db}
	schema, err := tool.LoadFromCSV(context.Background(), path, "numeric_data")
	if err != nil {
		t.Fatal(err)
	}
	var max float64
	var id, precise string
	if err := db.QueryRow(`SELECT CAST(MAX(CAST(amount AS DECIMAL(38,10))) AS DOUBLE), MIN(employee_id), MIN(precise_price) FROM numeric_data`).Scan(&max, &id, &precise); err != nil {
		t.Fatal(err)
	}
	if max != 100 || id != "001" || precise != "0.12345678901" {
		t.Fatalf("conversion changed data: %v %s %s", max, id, precise)
	}
	// VARCHAR ordering and numeric ordering are both valid operations. Binding
	// must preserve the chosen SQL semantics instead of guessing user intent.
	query, _, err := bindAnalysisSQL(`SELECT MAX(value) FROM data`, schema, "")
	if err != nil {
		t.Fatal(err)
	}
	var lexicalMax string
	if err := db.QueryRow(query).Scan(&lexicalMax); err != nil || lexicalMax != "9" {
		t.Fatalf("text aggregate semantics changed: %q, %v", lexicalMax, err)
	}
	if _, _, err := bindAnalysisSQL(`SELECT MAX(CAST(value AS DECIMAL(38,10))) FROM data`, schema, ""); err != nil {
		t.Fatal(err)
	}
}

func TestAnalysisResultLimitsReportIncompleteCoverage(t *testing.T) {
	db := regressionDuckDB(t)
	if _, err := db.Exec(`CREATE TABLE sample AS SELECT * FROM range(1002)`); err != nil {
		t.Fatal(err)
	}
	tool := &DataAnalysisTool{db: db, loadedSchemas: map[string]*TableSchema{"doc": {TableName: "sample"}}}
	for _, limit := range []int{0, 2} {
		payload, _ := json.Marshal(DataAnalysisInput{KnowledgeID: "doc", Sql: "SELECT * FROM data", MaxRows: limit})
		result, err := tool.Execute(context.Background(), payload)
		if err != nil {
			t.Fatal(err)
		}
		expected := limit
		if expected == 0 {
			expected = dataAnalysisDefaultRows
		}
		if result.Data["row_count"] != expected || result.Data["truncated"] != true || !strings.Contains(result.Output, "截断") {
			t.Fatalf("missing result bound: %+v", result.Data)
		}
	}
	rows, err := db.Query(`SELECT repeat('x',300000) AS huge`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	_, results, truncated, err := scanSQLRows(rows, 10, dataAnalysisMaxResultBytes)
	if err != nil || len(results) != 0 || !truncated {
		t.Fatalf("byte budget ignored: %v", err)
	}
}

type sqlRegressionKnowledgeService struct {
	interfaces.KnowledgeService
	knowledge *types.Knowledge
}

func (s sqlRegressionKnowledgeService) GetKnowledgeByIDOnly(context.Context, string) (*types.Knowledge, error) {
	return s.knowledge, nil
}

func TestParsedAnalysisCacheCoalescesAndReauthorizes(t *testing.T) {
	db := regressionDuckDB(t)
	var downloads atomic.Int32
	file := &fakeFileService{readers: map[string]func() (io.ReadCloser, error){"data.csv": func() (io.ReadCloser, error) {
		downloads.Add(1)
		return io.NopCloser(bytes.NewBufferString("amount\n9\n100\n")), nil
	}}}
	k := &types.Knowledge{ID: "cached", TenantID: 7, KnowledgeBaseID: "kb", CurrentVersionID: "v1", FilePath: "data.csv", FileType: "csv", UpdatedAt: time.Now()}
	a := NewDataAnalysisTool(nil, nil, nil, file, db, "same", InternalDataAnalysisAuthorization())
	b := NewDataAnalysisTool(nil, nil, nil, file, db, "same", InternalDataAnalysisAuthorization())
	defer a.Cleanup(context.Background())
	defer b.Cleanup(context.Background())
	var wg sync.WaitGroup
	for _, tool := range []*DataAnalysisTool{a, b} {
		wg.Add(1)
		go func(tool *DataAnalysisTool) {
			defer wg.Done()
			if _, err := tool.LoadFromKnowledge(context.Background(), k); err != nil {
				t.Error(err)
			}
		}(tool)
	}
	wg.Wait()
	if downloads.Load() != 1 {
		t.Fatalf("duplicate materialization: %d", downloads.Load())
	}
	blocked := NewDataAnalysisTool(nil, sqlRegressionKnowledgeService{knowledge: k}, nil, file, db, "other", AgentDataAnalysisAuthorization(types.SearchTargets{}, nil))
	if _, err := blocked.LoadFromKnowledgeID(context.Background(), k.ID); err == nil {
		t.Fatal("cache bypassed authorization")
	}
	changed := *k
	changed.CurrentVersionID = "v2"
	if _, err := b.LoadFromKnowledge(context.Background(), &changed); err != nil {
		t.Fatal(err)
	}
	if downloads.Load() != 2 {
		t.Fatal("new version reused old data")
	}
	parsedAnalysisCache.Lock()
	for _, knowledge := range []*types.Knowledge{k, &changed} {
		key := analysisCacheKey(a, knowledge)
		if entry := parsedAnalysisCache.entries[key]; entry != nil {
			os.Remove(entry.path)
			delete(parsedAnalysisCache.entries, key)
		}
	}
	parsedAnalysisCache.Unlock()
}

func TestParsedAnalysisCacheFallsBackWhenArtifactIsCorrupt(t *testing.T) {
	db := regressionDuckDB(t)
	var downloads atomic.Int32
	file := &fakeFileService{readers: map[string]func() (io.ReadCloser, error){"cache-fallback.csv": func() (io.ReadCloser, error) {
		downloads.Add(1)
		return io.NopCloser(strings.NewReader("value\n42\n")), nil
	}}}
	k := &types.Knowledge{ID: "cache-fallback", TenantID: 7, CurrentVersionID: "v1", FilePath: "cache-fallback.csv", FileType: "csv", UpdatedAt: time.Now()}
	first := NewDataAnalysisTool(nil, nil, nil, file, db, "first", InternalDataAnalysisAuthorization())
	if _, err := first.LoadFromKnowledge(context.Background(), k); err != nil {
		t.Fatal(err)
	}
	first.Cleanup(context.Background())

	key := analysisCacheKey(first, k)
	defer removeRegressionCacheEntry(key)
	parsedAnalysisCache.Lock()
	entry := parsedAnalysisCache.entries[key]
	parsedAnalysisCache.Unlock()
	if entry == nil {
		t.Fatal("expected parsed cache entry")
	}
	if err := os.WriteFile(entry.path, []byte("not parquet"), 0600); err != nil {
		t.Fatal(err)
	}

	second := NewDataAnalysisTool(nil, nil, nil, file, db, "second", InternalDataAnalysisAuthorization())
	defer second.Cleanup(context.Background())
	schema, err := second.LoadFromKnowledge(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}
	if schema.RowCount != 1 || downloads.Load() != 2 {
		t.Fatalf("corrupt cache did not fall back to source: rows=%d downloads=%d", schema.RowCount, downloads.Load())
	}
}

func TestParsedAnalysisCacheFallsBackWhenArtifactBodyIsCorrupt(t *testing.T) {
	db := regressionDuckDB(t)
	var downloads atomic.Int32
	file := &fakeFileService{readers: map[string]func() (io.ReadCloser, error){"cache-body.csv": func() (io.ReadCloser, error) {
		downloads.Add(1)
		var source strings.Builder
		source.WriteString("value\n")
		for i := 0; i < 2000; i++ {
			fmt.Fprintf(&source, "%d\n", i)
		}
		return io.NopCloser(strings.NewReader(source.String())), nil
	}}}
	k := &types.Knowledge{ID: "cache-body", TenantID: 7, CurrentVersionID: "v1", FilePath: "cache-body.csv", FileType: "csv", UpdatedAt: time.Now()}
	first := NewDataAnalysisTool(nil, nil, nil, file, db, "first-body", InternalDataAnalysisAuthorization())
	if _, err := first.LoadFromKnowledge(context.Background(), k); err != nil {
		t.Fatal(err)
	}
	first.Cleanup(context.Background())

	key := analysisCacheKey(first, k)
	defer removeRegressionCacheEntry(key)
	parsedAnalysisCache.Lock()
	entry := parsedAnalysisCache.entries[key]
	parsedAnalysisCache.Unlock()
	if entry == nil {
		t.Fatal("expected parsed cache entry")
	}
	artifact, err := os.ReadFile(entry.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact) < 128 {
		t.Fatalf("unexpectedly small parquet artifact: %d", len(artifact))
	}
	for i := len(artifact) / 3; i < len(artifact)/3+64; i++ {
		artifact[i] = 0xff
	}
	if err := os.WriteFile(entry.path, artifact, 0600); err != nil {
		t.Fatal(err)
	}
	if string(artifact[:4]) != "PAR1" || string(artifact[len(artifact)-4:]) != "PAR1" {
		t.Fatal("test must preserve parquet header and footer markers")
	}

	second := NewDataAnalysisTool(nil, nil, nil, file, db, "second-body", InternalDataAnalysisAuthorization())
	defer second.Cleanup(context.Background())
	schema, err := second.LoadFromKnowledge(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}
	if schema.RowCount != 2000 || downloads.Load() != 2 {
		t.Fatalf("body-corrupt cache did not fall back to source: rows=%d downloads=%d", schema.RowCount, downloads.Load())
	}
}

func removeRegressionCacheEntry(key string) {
	parsedAnalysisCache.Lock()
	defer parsedAnalysisCache.Unlock()
	if entry := parsedAnalysisCache.entries[key]; entry != nil {
		_ = os.Remove(entry.path)
		delete(parsedAnalysisCache.entries, key)
	}
}

func TestLoadFromKnowledgeSetCombinesAuthorizedSourcesAndPreservesColumns(t *testing.T) {
	db := regressionDuckDB(t)
	file := &fakeFileService{readers: map[string]func() (io.ReadCloser, error){
		"a.csv": func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("id,__source_file\n1,original\n2,original\n")), nil
		},
		"b.csv": func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("id,department\n3,quality\n")), nil
		},
	}}
	a := &types.Knowledge{ID: "a", FileName: "a.csv", FilePath: "a.csv", FileType: "csv"}
	b := &types.Knowledge{ID: "b", FileName: "b.csv", FilePath: "b.csv", FileType: "csv"}
	tool := NewDataAnalysisTool(nil, nil, nil, file, db, "combined", InternalDataAnalysisAuthorization())
	defer tool.Cleanup(context.Background())

	schema, err := tool.LoadFromKnowledgeSet(context.Background(), []*types.Knowledge{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if schema.RowCount != 3 || schema.Metadata["source_count"] != 2 {
		t.Fatalf("unexpected combined schema: %+v", schema)
	}
	sourceColumn, _ := schema.Metadata["source_file_column"].(string)
	if sourceColumn == dataSourceFileColumn {
		t.Fatal("source metadata overwrote an existing user column")
	}
	var sources int
	query := fmt.Sprintf("SELECT count(DISTINCT %s) FROM %s", quoteDuckDBIdentifier(sourceColumn), quoteDuckDBIdentifier(schema.TableName))
	if err := db.QueryRow(query).Scan(&sources); err != nil || sources != 2 {
		t.Fatalf("combined provenance missing: count=%d err=%v", sources, err)
	}
	// Keep the materialization connection busy so execution uses another pooled connection.
	materializationConn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer materializationConn.Close()
	if err := db.QueryRow(query).Scan(&sources); err != nil || sources != 2 {
		t.Fatalf("combined dataset unavailable on another connection: count=%d err=%v", sources, err)
	}
}

func TestDatabaseVisibilityUsesOneQueryAndVersionWindow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "scope.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := db.DB()
	defer raw.Close()
	for _, ddl := range []string{
		`CREATE TABLE knowledges(id TEXT,knowledge_base_id TEXT,tenant_id INTEGER,current_version_id TEXT,pending_version_id TEXT,tag_id TEXT,deleted_at DATETIME)`,
		`CREATE TABLE knowledge_versions(id TEXT,knowledge_id TEXT,tenant_id INTEGER,status TEXT,effective_at DATETIME,expires_at DATETIME)`,
		`INSERT INTO knowledges VALUES ('legacy','kb',7,'','','on',NULL),('active','kb',7,'v1','','on',NULL),('expired','kb',7,'v2','','on',NULL),('other','kb',7,'','','off',NULL)`,
		`INSERT INTO knowledge_versions VALUES ('v1','active',7,'active',NULL,NULL),('v2','expired',7,'expired',NULL,NULL)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatal(err)
		}
	}
	var queries int
	if err := db.Callback().Query().Before("gorm:query").Register("count_sql_scope", func(*gorm.DB) { queries++ }); err != nil {
		t.Fatal(err)
	}
	tool := NewDatabaseQueryToolWithGovernance(db, types.SearchTargets{{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb", TenantID: 7, TagIDs: []string{"on"}}}, agentVisibilityGovernanceRepo{})
	ids, versions, err := tool.visibleKnowledgeVersions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || versions["active"] != "v1" || queries != 1 {
		t.Fatalf("scope or query count wrong: %v %v %d", ids, versions, queries)
	}
	for _, id := range ids {
		if id != "active" && id != "legacy" {
			t.Fatalf("unexpected visible ID %s", id)
		}
	}
}

func TestExcelMetadataFailureIsNotFirstSheetSuccess(t *testing.T) {
	tool := &DataAnalysisTool{db: regressionDuckDB(t)}
	if _, err := tool.LoadFromExcel(context.Background(), filepath.Join(t.TempDir(), "missing.xlsx"), "missing"); err == nil {
		t.Fatal("metadata failure silently accepted")
	}
	if len(tool.createdTables) != 0 {
		t.Fatal("failed creation claimed ownership")
	}
}

func TestMaterializationRejectsOversizedAndCanceledFiles(t *testing.T) {
	tool := &DataAnalysisTool{}
	if _, err := tool.LoadFromKnowledge(context.Background(), &types.Knowledge{FileSize: utils.GetMaxFileSize() + 1}); err == nil {
		t.Fatal("oversized file accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	file := &fakeFileService{readers: map[string]func() (io.ReadCloser, error){"source": func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("data")), nil }}}
	tool.fileService = file
	if _, cleanup, err := tool.materializeKnowledgeFile(ctx, &types.Knowledge{FilePath: "source"}); err == nil {
		cleanup()
		t.Fatal("canceled copy accepted")
	}
}

func TestDuplicateResultColumnDoesNotOverwriteEvidence(t *testing.T) {
	db := regressionDuckDB(t)
	rows, err := db.Query("SELECT 1 AS value, 2 AS value")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if _, _, _, err := scanSQLRows(rows, 10, 1000); err == nil {
		t.Fatal(fmt.Sprint("duplicate aliases silently overwrite values"))
	}
}

func TestAnalysisGroundsCredentialCountsAcrossColumns(t *testing.T) {
	db := regressionDuckDB(t)
	_, err := db.Exec(`CREATE TABLE credentials AS SELECT CAST(i AS VARCHAR) AS employee_id,
 CASE WHEN i <= 41 THEN '软件评测师（软考）' ELSE '' END AS "职称专业",
 CASE WHEN i <= 42 THEN '计算机技术与软件专业技术资格 软件评测师' ELSE '高级软件测评师（培训证）' END AS "专业证书"
 FROM range(1,44) AS people(i)`)
	if err != nil {
		t.Fatal(err)
	}
	tool := &DataAnalysisTool{db: db}
	schema, err := tool.profileAnalysisTable(context.Background(), "credentials")
	if err != nil {
		t.Fatal(err)
	}
	description := schema.Description()
	for _, value := range []string{"软件评测师（软考）", "计算机技术与软件专业技术资格 软件评测师", "高级软件测评师（培训证）"} {
		if !strings.Contains(description, value) {
			t.Fatalf("stored terminology missing: %s", value)
		}
	}
	tool.loadedSchemas = map[string]*TableSchema{"doc": schema}
	payload, _ := json.Marshal(DataAnalysisInput{KnowledgeID: "doc", Sql: `SELECT COUNT(DISTINCT CASE WHEN "职称专业" LIKE '%软件评测师%' THEN employee_id END) AS title_count, COUNT(DISTINCT CASE WHEN "专业证书" LIKE '%软件评测师%' THEN employee_id END) AS certificate_count, COUNT(DISTINCT CASE WHEN "职称专业" LIKE '%软件评测师%' OR "专业证书" LIKE '%软件评测师%' THEN employee_id END) AS combined_count FROM data`})
	result, err := tool.Execute(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []string{"41", "42"} {
		if !strings.Contains(result.Output, count) {
			t.Fatalf("missing full-table count %s: %s", count, result.Output)
		}
	}
}

func TestSQLRewritingPreservesOutputAliasesAndAuthorizedSelfJoin(t *testing.T) {
	db := regressionDuckDB(t)
	if _, err := db.Exec(`CREATE TABLE physical(ab VARCHAR); INSERT INTO physical VALUES ('1'),('2')`); err != nil {
		t.Fatal(err)
	}
	schema := &TableSchema{TableName: "physical", Columns: []ColumnInfo{{Name: "ab", AnalysisType: "numeric text"}}}
	query, _, err := bindAnalysisSQL(`SELECT -CAST(ab AS INTEGER) AS "a b" FROM data ORDER BY "a b"`, schema, "")
	if err != nil {
		t.Fatal(err)
	}
	var value int
	if err := db.QueryRow(query).Scan(&value); err != nil || value != -2 {
		t.Fatalf("output alias was rebound: %d %v", value, err)
	}
	query, _, err = bindAnalysisSQL(`SELECT COUNT(*) FROM data a JOIN data b ON a.ab=b.ab`, schema, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateDataAnalysisSQL(query, schema.TableName); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(query).Scan(&value); err != nil || value != 2 {
		t.Fatalf("authorized self join changed: %d %v", value, err)
	}
}
func TestSQLIsolationPreservesSchemaQualifiedReferences(t *testing.T) {
	db := regressionDuckDB(t)
	if _, err := db.Exec(`CREATE SCHEMA public; CREATE TABLE public.knowledges(id VARCHAR,tenant_id INTEGER); INSERT INTO public.knowledges VALUES ('allowed',1),('denied',2)`); err != nil {
		t.Fatal(err)
	}
	query, _, err := utils.ValidateAndSecureSQL(`SELECT public.knowledges.id FROM public.knowledges ORDER BY public.knowledges.id`, utils.WithSecurityDefaults(1))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		if id != "allowed" {
			t.Fatal("tenant isolation lost")
		}
		count++
	}
	if count != 1 {
		t.Fatalf("unexpected count %d", count)
	}
}
