package tools

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	filesvc "github.com/Tencent/WeKnora/internal/application/service/file"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
)

var dataAnalysisTool = BaseTool{
	name: ToolDataAnalysis,
	description: "Use this tool when the knowledge is CSV or Excel files. It loads the data into memory and executes SQL for data analysis. " +
		"For Excel files with multiple sheets, every sheet is loaded into the same table and the source sheet name is exposed as a '__sheet_name' column so you can filter/aggregate per sheet. " +
		"Use SQL for detail retrieval, filtering, sorting, grouping, calculations and aggregation. Preserve the user's requested operation; do not replace a detail query with statistics.",
	schema: utils.GenerateSchema[DataAnalysisInput](),
}

// excelSheetNameColumn is the name of the synthetic column that identifies
// which Excel sheet a row came from when multiple sheets are unioned together.
const excelSheetNameColumn = "__sheet_name"

const (
	dataSourceFileColumn      = "__source_file"
	dataSourceKnowledgeColumn = "__source_knowledge_id"
)

const (
	dataAnalysisQueryTimeout            = 10 * time.Second
	maxSQLIdentifierLength              = 63
	dataAnalysisMaxCacheFileBytes int64 = 64 << 20
	dataAnalysisDefaultRows             = 1000
	dataAnalysisMaxResultBytes          = 256 << 10
)

// sqlSingleQuoteEscape escapes single quotes in a string so it can be safely
// embedded inside a single-quoted SQL literal.
func sqlSingleQuoteEscape(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func normalizeIdentifierForMatch(s string) string {
	normalized := strings.ToLower(strings.TrimSpace(s))
	normalized = strings.ReplaceAll(normalized, " ", "")
	normalized = strings.ReplaceAll(normalized, "\u3000", "")
	return normalized
}

func quoteDuckDBIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func buildMissingColumnSuggestion(sqlErr error, schema *TableSchema) string {
	if sqlErr == nil || schema == nil {
		return ""
	}
	msg := sqlErr.Error()
	if !strings.Contains(msg, `Referenced column "`) || !strings.Contains(msg, `not found`) {
		return ""
	}

	matches := regexp.MustCompile(`Referenced column "([^"]+)" not found`).FindStringSubmatch(msg)
	if len(matches) < 2 {
		return ""
	}

	missing := matches[1]
	normalizedMissing := normalizeIdentifierForMatch(missing)
	if normalizedMissing == "" {
		return ""
	}

	for _, col := range schema.Columns {
		if normalizeIdentifierForMatch(col.Name) == normalizedMissing {
			return fmt.Sprintf("Column %q does not exist. Did you mean %q? Please use the exact column name from schema.", missing, col.Name)
		}
	}

	return ""
}

type DataAnalysisAction string

const (
	DataAnalysisActionExecute DataAnalysisAction = "execute"
	DataAnalysisActionSkip    DataAnalysisAction = "skip"
	DataAnalysisActionClarify DataAnalysisAction = "clarify"
)

type DataAnalysisInput struct {
	Action      DataAnalysisAction `json:"action" jsonschema:"Required action: execute to run SQL, skip when table analysis is unnecessary, or clarify when the requested scope or value interpretation is ambiguous"`
	KnowledgeID string             `json:"knowledge_id" jsonschema:"id of the knowledge to query"`
	Sql         string             `json:"sql" jsonschema:"SQL to be executed on knowledge"`
	MaxRows     int                `json:"max_rows,omitempty" jsonschema:"optional maximum rows returned by a read-only SELECT query"`
}

type dataAnalysisAuthorizationMode uint8

const (
	dataAnalysisAuthorizationInvalid dataAnalysisAuthorizationMode = iota
	dataAnalysisAuthorizationAgentScope
	dataAnalysisAuthorizationInternal
)

// DataAnalysisAuthorization makes the caller's authorization responsibility
// explicit. Values can only be created by the constructors below.
type DataAnalysisAuthorization struct {
	mode           dataAnalysisAuthorizationMode
	searchTargets  types.SearchTargets
	governanceRepo interfaces.KnowledgeGovernanceRepository
}

func AgentDataAnalysisAuthorization(searchTargets types.SearchTargets, governanceRepo interfaces.KnowledgeGovernanceRepository) DataAnalysisAuthorization {
	if len(searchTargets) == 0 {
		return DataAnalysisAuthorization{}
	}
	return DataAnalysisAuthorization{mode: dataAnalysisAuthorizationAgentScope, searchTargets: searchTargets, governanceRepo: governanceRepo}
}

// InternalDataAnalysisAuthorization is for callers that have already
// authorized the exact knowledge before invoking the tool.
func InternalDataAnalysisAuthorization() DataAnalysisAuthorization {
	return DataAnalysisAuthorization{mode: dataAnalysisAuthorizationInternal}
}

type DataAnalysisTool struct {
	BaseTool
	knowledgeBaseService interfaces.KnowledgeBaseService
	knowledgeService     interfaces.KnowledgeService
	fileService          interfaces.FileService
	tenantService        interfaces.TenantService
	authorization        DataAnalysisAuthorization
	db                   *sql.DB
	sessionID            string
	instanceID           string
	identityOnce         sync.Once
	operationMu          sync.Mutex
	createdTables        []string // Track tables created in this session
	loadedSchemas        map[string]*TableSchema
}

func NewDataAnalysisTool(
	knowledgeBaseService interfaces.KnowledgeBaseService,
	knowledgeService interfaces.KnowledgeService,
	tenantService interfaces.TenantService,
	fileService interfaces.FileService,
	db *sql.DB,
	sessionID string,
	authorization DataAnalysisAuthorization,
) *DataAnalysisTool {
	return &DataAnalysisTool{
		BaseTool:             dataAnalysisTool,
		knowledgeBaseService: knowledgeBaseService,
		knowledgeService:     knowledgeService,
		fileService:          fileService,
		tenantService:        tenantService,
		authorization:        authorization,
		db:                   db,
		sessionID:            sessionID,
		loadedSchemas:        make(map[string]*TableSchema),
	}
}

// recordCreatedTable records a table name for cleanup, ensuring uniqueness
// Returns true if the table was newly recorded, false if it already existed
func (t *DataAnalysisTool) recordCreatedTable(tableName string) bool {
	for _, name := range t.createdTables {
		if name == tableName {
			return false
		}
	}
	t.createdTables = append(t.createdTables, tableName)
	return true
}

// Cleanup cleans up the session-specific schema
func (t *DataAnalysisTool) Cleanup(ctx context.Context) {
	t.operationMu.Lock()
	defer t.operationMu.Unlock()
	if len(t.createdTables) == 0 {
		logger.Infof(ctx, "[Tool][DataAnalysis] No tables to clean up for session: %s", t.sessionID)
		return
	}

	logger.Infof(ctx, "[Tool][DataAnalysis] Cleaning up %d tables for session: %s", len(t.createdTables), t.sessionID)

	for _, tableName := range t.createdTables {
		dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS \"%s\"", tableName)
		if _, err := t.db.ExecContext(ctx, dropSQL); err != nil {
			logger.Errorf(ctx, "[Tool][DataAnalysis] Failed to drop table '%s': %v", tableName, err)
			// Continue to drop other tables even if one fails
			continue
		}
		logger.Infof(ctx, "[Tool][DataAnalysis] Successfully dropped table '%s'", tableName)
	}

	// Clear the list after cleanup
	t.createdTables = nil
	t.loadedSchemas = make(map[string]*TableSchema)
}

// Execute executes the SQL query on DuckDB (only read-only queries are allowed)
func (t *DataAnalysisTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	t.operationMu.Lock()
	defer t.operationMu.Unlock()
	logger.Infof(ctx, "[Tool][DataAnalysis] Execute started for session: %s", t.sessionID)
	var input DataAnalysisInput
	if err := json.Unmarshal(args, &input); err != nil {
		logger.Errorf(ctx, "[Tool][DataAnalysis] Failed to parse input args: %v", err)
		return &types.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to parse input args: %v", err),
		}, err
	}

	schema, err := t.loadFromKnowledgeID(ctx, input.KnowledgeID)
	if err != nil {
		logger.Errorf(ctx, "[Tool][DataAnalysis] Failed to load knowledge ID '%s': %v", input.KnowledgeID, err)
		return &types.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to load knowledge ID '%s': %v", input.KnowledgeID, err),
		}, err
	}

	boundSQL, fixes, err := bindAnalysisSQL(input.Sql, schema, input.KnowledgeID)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, err
	}
	input.Sql = boundSQL
	if len(fixes) > 0 {
		logger.Infof(ctx, "[Tool][DataAnalysis] Bound SQL columns: %v", fixes)
	}

	// Integration and agent callers may only execute SELECT statements.
	normalizedSQL := strings.TrimSpace(strings.ToLower(input.Sql))
	if !strings.HasPrefix(normalizedSQL, "select") {
		// Reject modification queries
		logger.Warnf(ctx, "[Tool][DataAnalysis] Modification query rejected for session %s: %s", t.sessionID, input.Sql)
		return &types.ToolResult{
			Success: false,
			Error:   "DuckDB tool only supports SELECT queries.",
		}, fmt.Errorf("modification queries are not allowed")
	}

	// Validate SQL with comprehensive security checks
	// IMPORTANT: Must enable validateSelectStmt to block RangeFunction attacks
	if err := validateDataAnalysisSQL(input.Sql, schema.TableName); err != nil {
		logger.Warnf(ctx, "[Tool][DataAnalysis] SQL validation failed for session %s: %v", t.sessionID, err)
		return &types.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("SQL validation failed: %v", err),
		}, fmt.Errorf("SQL validation failed: %w", err)
	}

	executionSQL := input.Sql
	if input.MaxRows < 0 || input.MaxRows > 10000 {
		return &types.ToolResult{Success: false, Error: "max_rows must be between 1 and 10000"}, fmt.Errorf("invalid max_rows")
	}
	if input.MaxRows == 0 {
		input.MaxRows = dataAnalysisDefaultRows
	}
	if input.MaxRows > 0 {
		executionSQL = fmt.Sprintf("SELECT * FROM (%s) AS limited_result LIMIT %d", strings.TrimSpace(strings.TrimSuffix(input.Sql, ";")), input.MaxRows+1)
	}
	logger.Infof(ctx, "[Tool][DataAnalysis] Received SQL query for session %s: %s", t.sessionID, executionSQL)
	// Execute single query and get results
	queryCtx, cancel := context.WithTimeout(ctx, dataAnalysisQueryTimeout)
	defer cancel()
	results, truncated, err := t.executeBoundedQuery(queryCtx, executionSQL, input.MaxRows)
	if err != nil {
		if suggestion := buildMissingColumnSuggestion(err, schema); suggestion != "" {
			return &types.ToolResult{
				Success: false,
				Error:   fmt.Sprintf("Query execution failed: %v. %s", err, suggestion),
			}, err
		}
		return &types.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Query execution failed: %v", err),
		}, err
	}

	queryOutput := t.formatQueryResults(results, input.Sql)
	if truncated {
		if len(results) == 0 {
			queryOutput = strings.ReplaceAll(queryOutput, "No matching records found.", "匹配记录超出展示字节限制，不能视为零条匹配。")
		}
		queryOutput += "\n结果已截断：当前展示不是完整结果。请分页获取明细；仅在需要统计时使用聚合查询。\n"
	}
	logger.Infof(ctx, "[Tool][DataAnalysis] Completed execution query, total %d rows for session %s", len(results), t.sessionID)
	return &types.ToolResult{
		Success: true,
		Output:  queryOutput,
		Data: map[string]interface{}{
			"rows":         results,
			"row_count":    len(results),
			"query":        input.Sql,
			"truncated":    truncated,
			"max_rows":     input.MaxRows,
			"display_type": ToolDataAnalysis,
			"session_id":   t.sessionID,
		},
	}, nil
}

func validateDataAnalysisSQL(sqlText, tableName string) error {
	_, validation := utils.ValidateSQL(sqlText,
		utils.WithAllowedTables(tableName),
		utils.WithSelectOnly(),
		utils.WithNoSubqueries(),
		utils.WithNoCTEs(),
		utils.WithSingleStatement(),      // Block multiple statements
		utils.WithNoDangerousFunctions(), // Block dangerous functions
		utils.WithDefaultSafeFunctions(), // Block DuckDB external I/O and unapproved functions
		utils.WithAdditionalSafeFunctions("regexp_matches", "median", "stddev", "stddev_pop", "stddev_samp"), // DuckDB scalar/aggregate functions without external I/O
	)
	if !validation.Valid {
		return fmt.Errorf("%v", validation.Errors)
	}
	return nil
}

// executeSingleQuery executes a single SQL query and returns columns and results
// Parameters:
//   - ctx: context for cancellation and timeout
//   - sqlQuery: the SQL query to execute
//   - existingColumns: existing column names to merge with (can be nil or empty)
//
// Returns:
//   - []string: merged column names (existing + new columns, deduplicated)
//   - []map[string]string: query results
//   - error: any error that occurred during execution
func (t *DataAnalysisTool) executeSingleQuery(ctx context.Context, sqlQuery string, args ...interface{}) ([]map[string]string, error) {
	result, _, err := t.executeBoundedQuery(ctx, sqlQuery, dataAnalysisDefaultRows, args...)
	return result, err
}

func (t *DataAnalysisTool) executeBoundedQuery(ctx context.Context, query string, limit int, args ...interface{}) ([]map[string]string, bool, error) {
	rows, err := t.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	_, raw, truncated, err := scanSQLRows(rows, limit, dataAnalysisMaxResultBytes)
	if err != nil {
		return nil, false, err
	}
	results := make([]map[string]string, 0, len(raw))
	for _, row := range raw {
		converted := make(map[string]string, len(row))
		for name, value := range row {
			if value == nil {
				converted[name] = "NULL"
			} else {
				converted[name] = fmt.Sprint(value)
			}
		}
		results = append(results, converted)
	}
	return results, truncated, nil
}

// formatQueryResults formats query results into JSONL format (one JSON object per line)
func (t *DataAnalysisTool) formatQueryResults(results []map[string]string, query string) string {
	var output strings.Builder

	output.WriteString("=== DuckDB Query Results ===\n\n")
	output.WriteString(fmt.Sprintf("Executed SQL: %s\n\n", query))
	output.WriteString(fmt.Sprintf("Returned %d rows\n\n", len(results)))

	if len(results) == 0 {
		output.WriteString("No matching records found.\n")
		return output.String()
	}

	output.WriteString("=== Data Details ===\n\n")
	if len(results) > 10 {
		output.WriteString(fmt.Sprintf("Showing %d returned records. Check the truncation flag before treating these as complete.\n\n", len(results)))
	}

	// Write each record as a separate JSON line
	for i, record := range results {
		recordBytes, _ := json.Marshal(record)

		// Remove the trailing newline added by Encode
		recordStr := strings.Trim(string(recordBytes), "\n")
		output.WriteString(fmt.Sprintf("record %d: %s\n", i+1, recordStr))
	}

	return output.String()
}

// TableSchema represents the schema information of a table
type TableSchema struct {
	TableName string                 `json:"table_name"`
	Columns   []ColumnInfo           `json:"columns"`
	RowCount  int64                  `json:"row_count"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// ColumnInfo represents information about a single column
type ColumnInfo struct {
	Name          string   `json:"name"`
	Type          string   `json:"type"`
	Nullable      string   `json:"nullable"`
	AnalysisType  string   `json:"analysis_type,omitempty"`
	ValueExamples []string `json:"value_examples,omitempty"`
	Multiline     bool     `json:"multiline,omitempty"`
}

// LoadFromCSV loads data from a CSV file into a DuckDB table and returns the table schema
// Parameters:
//   - ctx: context for cancellation and timeout
//   - filename: path to the CSV file
//   - tableName: name of the table to create
//
// Returns:
//   - *TableSchema: schema information of the created table
//   - error: any error that occurred during the operation
func (t *DataAnalysisTool) LoadFromCSV(ctx context.Context, filename string, tableName string) (*TableSchema, error) {
	logger.Infof(ctx, "[Tool][DataAnalysis] Loading CSV file '%s' into table '%s' for session %s", filename, tableName, t.sessionID)

	// Record the created table for cleanup. If already exists, skip creation
	if !t.hasCreatedTable(tableName) {
		// Create table from CSV using DuckDB's read_csv_auto function
		// with explicit header detection and VARCHAR coercion to align with
		// Excel loading behavior.
		// Table will be created in the session schema
		createTableSQL := fmt.Sprintf(
			"CREATE TABLE \"%s\" AS SELECT * FROM read_csv_auto('%s', header=true, all_varchar=true)",
			tableName, sqlSingleQuoteEscape(filename),
		)

		_, err := t.db.ExecContext(ctx, createTableSQL)
		if err != nil {
			logger.Errorf(ctx, "[Tool][DataAnalysis] Failed to create table from CSV: %v", err)
			return nil, fmt.Errorf("failed to create table from CSV: %w", err)
		}

		t.recordCreatedTable(tableName)
		logger.Infof(ctx, "[Tool][DataAnalysis] Successfully created table '%s' from CSV file in session %s", tableName, t.sessionID)
	}

	// Get and return the table schema
	return t.profileAnalysisTable(ctx, tableName)
}

// LoadFromExcel loads data from an Excel file into a DuckDB table and returns the table schema.
//
// Multi-sheet workbooks are fully supported: every sheet in the workbook is
// loaded and the rows from all sheets are unioned (UNION ALL BY NAME) into a
// single table. A synthetic '__sheet_name' column is added so downstream SQL
// can filter / aggregate per sheet. Enumeration errors abort analysis so a
// partial workbook can never be presented as a complete result.
//
// Parameters:
//   - ctx: context for cancellation and timeout
//   - filename: path to the Excel file
//   - tableName: name of the table to create
//
// Returns:
//   - *TableSchema: schema information of the created table
//   - error: any error that occurred during the operation
//
// Note: requires the DuckDB 'excel' extension for read_xlsx.
func (t *DataAnalysisTool) LoadFromExcel(ctx context.Context, filename string, tableName string) (*TableSchema, error) {
	logger.Infof(ctx, "[Tool][DataAnalysis] Loading Excel file '%s' into table '%s' for session %s", filename, tableName, t.sessionID)

	// Record the created table for cleanup. If already exists, skip creation.
	if !t.hasCreatedTable(tableName) {
		sheetNames, enumErr := t.listExcelSheets(ctx, filename)
		if enumErr != nil || len(sheetNames) == 0 {
			return nil, fmt.Errorf("cannot enumerate every Excel sheet: %v", enumErr)
		}

		analysisPath, cleanup, err := prepareAnalysisExcel(ctx, filename)
		if err != nil {
			return nil, fmt.Errorf("prepare merged Excel cells: %w", err)
		}
		defer cleanup()
		createTableSQL := buildExcelCreateTableSQL(tableName, analysisPath, sheetNames)

		if _, err := t.db.ExecContext(ctx, createTableSQL); err != nil {
			logger.Errorf(ctx, "[Tool][DataAnalysis] Failed to create table from Excel (sheets=%v): %v", sheetNames, err)
			return nil, fmt.Errorf("failed to create table from Excel file (sheets=%v): %w", sheetNames, err)
		}

		t.recordCreatedTable(tableName)
		logger.Infof(ctx,
			"[Tool][DataAnalysis] Successfully created table '%s' from Excel file in session %s (sheets=%v)",
			tableName, t.sessionID, sheetNames,
		)
	}

	// Get and return the table schema
	return t.profileAnalysisTable(ctx, tableName)
}

// listExcelSheets reads workbook metadata in on-disk sheet order.
func (t *DataAnalysisTool) listExcelSheets(ctx context.Context, filename string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	workbook, err := excelize.OpenFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read Excel sheet metadata: %w", err)
	}
	defer workbook.Close()
	return workbook.GetSheetList(), nil
}

// buildExcelCreateTableSQL assembles the CREATE TABLE statement used by
// LoadFromExcel. Exposed at package level (lower-case) to make it trivially
// testable without a live DuckDB connection.
func buildExcelCreateTableSQL(tableName, filename string, sheetNames []string) string {
	escFile := sqlSingleQuoteEscape(filename)

	// No sheet info (enumeration failed or empty): read the first sheet only.
	if len(sheetNames) == 0 {
		return fmt.Sprintf(
			"CREATE TABLE \"%s\" AS SELECT * FROM read_xlsx('%s', header=true, all_varchar=true)",
			tableName, escFile,
		)
	}

	// Single sheet: keep it simple but still tag the source for consistency
	// with the multi-sheet path.
	if len(sheetNames) == 1 {
		escSheet := sqlSingleQuoteEscape(sheetNames[0])
		return fmt.Sprintf(
			"CREATE TABLE \"%s\" AS SELECT *, '%s' AS %s FROM read_xlsx('%s', sheet = '%s', header=true, all_varchar=true)",
			tableName, escSheet, excelSheetNameColumn, escFile, escSheet,
		)
	}

	// Multiple sheets: UNION ALL BY NAME tolerates schema differences
	// between sheets (missing columns become NULL, conflicting types are
	// widened).
	parts := make([]string, 0, len(sheetNames))
	for _, sheet := range sheetNames {
		escSheet := sqlSingleQuoteEscape(sheet)
		parts = append(parts, fmt.Sprintf(
			"SELECT *, '%s' AS %s FROM read_xlsx('%s', sheet = '%s', header=true, all_varchar=true)",
			escSheet, excelSheetNameColumn, escFile, escSheet,
		))
	}
	return fmt.Sprintf(
		"CREATE TABLE \"%s\" AS %s",
		tableName,
		strings.Join(parts, "\nUNION ALL BY NAME\n"),
	)
}

// LoadFromKnowledge loads data from a Knowledge entity into a DuckDB table and returns the table schema.
// It automatically determines the file type and calls the appropriate loading method.
//
// The source file is first materialized to a local temp file via FileService.GetFile
// so DuckDB's st_read / read_xlsx / read_csv_auto can open it directly. This
// side-steps provider-specific URL schemes (e.g. the local:// URL returned by
// the local file service) that DuckDB's extensions cannot resolve on their own.
//
// Parameters:
//   - ctx: context for cancellation and timeout
//   - knowledge: the Knowledge entity containing file information
//
// Returns:
//   - *TableSchema: schema information of the created table
//   - error: any error that occurred during the operation
func (t *DataAnalysisTool) loadKnowledgeFile(ctx context.Context, knowledge *types.Knowledge) (*TableSchema, error) {
	if knowledge == nil {
		return nil, fmt.Errorf("knowledge cannot be nil")
	}
	if t.loadedSchemas == nil {
		t.loadedSchemas = make(map[string]*TableSchema)
	}
	tableName := t.TableName(knowledge)

	fileType := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(knowledge.FileType), "."))

	logger.Infof(ctx, "[Tool][DataAnalysis] Loading knowledge '%s' (type: %s) into table '%s' for session %s",
		knowledge.ID, fileType, tableName, t.sessionID)

	localPath, cleanup, err := t.materializeKnowledgeFile(ctx, knowledge)
	if err != nil {
		return nil, fmt.Errorf("failed to materialize knowledge '%s' for DuckDB: %w", knowledge.ID, err)
	}
	defer cleanup()
	if fileType == "xls" {
		convertedPath, convertedCleanup, convertErr := convertLegacyXLS(ctx, localPath)
		if convertErr != nil {
			return nil, fmt.Errorf("failed to convert legacy XLS knowledge '%s': %w", knowledge.ID, convertErr)
		}
		defer convertedCleanup()
		localPath = convertedPath
	}

	var schema *TableSchema
	switch fileType {
	case "csv":
		schema, err = t.LoadFromCSV(ctx, localPath, tableName)
	case "xlsx", "xls":
		schema, err = t.LoadFromExcel(ctx, localPath, tableName)
	default:
		logger.Warnf(ctx, "[Tool][DataAnalysis] Unsupported file type '%s' for knowledge '%s' in session %s",
			fileType, knowledge.ID, t.sessionID)
		return nil, fmt.Errorf("unsupported file type: %s (supported types: csv, xlsx, xls)", fileType)
	}
	if err == nil {
		t.loadedSchemas[knowledgeSchemaCacheKey(knowledge)] = schema
	}
	return schema, err
}

func convertLegacyXLS(ctx context.Context, sourcePath string) (string, func(), error) {
	soffice, err := exec.LookPath("soffice")
	if err != nil {
		return "", func() {}, fmt.Errorf("LibreOffice is required for XLS analysis")
	}
	outDir, err := os.MkdirTemp("", "weknora-xls-convert-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create conversion directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(outDir) }
	profileDir := filepath.Join(outDir, "profile")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("create LibreOffice profile: %w", err)
	}
	profileURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(profileDir)}).String()
	command := exec.CommandContext(ctx, soffice, "-env:UserInstallation="+profileURL, "--headless", "--convert-to", "xlsx", "--outdir", outDir, sourcePath)
	if output, commandErr := command.CombinedOutput(); commandErr != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("LibreOffice conversion failed: %w (%s)", commandErr, strings.TrimSpace(string(output)))
	}
	convertedPath := filepath.Join(outDir, strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath))+".xlsx")
	if info, statErr := os.Stat(convertedPath); statErr != nil || info.Size() == 0 {
		cleanup()
		return "", func() {}, fmt.Errorf("LibreOffice did not produce a valid XLSX file")
	}
	return convertedPath, cleanup, nil
}

// materializeKnowledgeFile copies the knowledge's backing blob into a fresh
// temp file on the local filesystem so DuckDB can open it with ordinary path
// semantics. It returns the temp path and a cleanup closure that removes the
// temp file; the closure is always safe to call and is a no-op on failure.
//
// This hides storage-backend-specific URL schemes (local://, oss://, s3://,
// minio://, cos://, …) behind the FileService.GetFile abstraction, so the
// Data Analysis tool works identically across all deployments.
func (t *DataAnalysisTool) materializeKnowledgeFile(ctx context.Context, knowledge *types.Knowledge) (string, func(), error) {
	noop := func() {}

	reader, err := t.resolveFileServiceForKnowledge(ctx, knowledge).GetFile(ctx, knowledge.FilePath)
	if err != nil {
		return "", noop, fmt.Errorf("failed to open file for knowledge '%s': %w", knowledge.ID, err)
	}
	defer reader.Close()

	// Preserve the file extension so DuckDB's format auto-detection still
	// works (e.g. the CSV reader expects .csv, xlsx reader expects .xlsx).
	suffix := ""
	if ext := strings.ToLower(strings.TrimSpace(knowledge.FileType)); ext != "" {
		suffix = "." + ext
	}

	tmp, err := os.CreateTemp("", "weknora-data-analysis-*"+suffix)
	if err != nil {
		return "", noop, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		// Best-effort cleanup; a missing file is fine, any other error is
		// only logged to avoid masking the original operation's result.
		if err := os.Remove(tmpPath); err != nil && !os.IsNotExist(err) {
			logger.Warnf(ctx, "[Tool][DataAnalysis] Failed to remove temp file %s: %v", tmpPath, err)
		}
	}

	stopClose := context.AfterFunc(ctx, func() { _ = reader.Close() })
	defer stopClose()
	copied, copyErr := io.Copy(tmp, io.LimitReader(reader, utils.GetMaxFileSize()+1))
	if copyErr == nil {
		copyErr = ctx.Err()
	}
	if copyErr == nil && copied > utils.GetMaxFileSize() {
		copyErr = fmt.Errorf("file exceeds %d byte analysis limit", utils.GetMaxFileSize())
	}
	if err := copyErr; err != nil {
		_ = tmp.Close()
		cleanup()
		return "", noop, fmt.Errorf("failed to copy knowledge '%s' to temp file: %w", knowledge.ID, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", noop, fmt.Errorf("failed to finalize temp file for knowledge '%s': %w", knowledge.ID, err)
	}

	logger.Infof(ctx, "[Tool][DataAnalysis] Materialized knowledge '%s' to temp file %s for session %s",
		knowledge.ID, tmpPath, t.sessionID)

	return tmpPath, cleanup, nil
}

// LoadFromKnowledgeID loads data from a Knowledge ID into a DuckDB table and returns the table schema
// Parameters:
//   - ctx: context for cancellation and timeout
//   - knowledgeID: the ID of the Knowledge entity
//
// Returns:
//   - string: the name of the created table
//   - *TableSchema: schema information of the created table
//   - error: any error that occurred during the operation
func (t *DataAnalysisTool) LoadFromKnowledgeID(ctx context.Context, knowledgeID string) (*TableSchema, error) {
	t.operationMu.Lock()
	defer t.operationMu.Unlock()
	return t.loadFromKnowledgeID(ctx, knowledgeID)
}

// LoadFromKnowledgeSet creates one query table from all authorized, retrieved
// structured sources. UNION ALL BY NAME preserves heterogeneous schemas while
// provenance columns keep every row auditable. The first knowledge ID remains
// the execution handle, so the existing authorization boundary stays intact.
func (t *DataAnalysisTool) LoadFromKnowledgeSet(ctx context.Context, knowledges []*types.Knowledge) (*TableSchema, error) {
	t.operationMu.Lock()
	defer t.operationMu.Unlock()
	if len(knowledges) == 0 {
		return nil, fmt.Errorf("at least one knowledge source is required")
	}
	for _, knowledge := range knowledges {
		if knowledge == nil {
			return nil, fmt.Errorf("knowledge source cannot be nil")
		}
		switch t.authorization.mode {
		case dataAnalysisAuthorizationAgentScope:
			if !agentKnowledgeVisible(ctx, knowledge, t.authorization.searchTargets, t.authorization.governanceRepo) {
				return nil, fmt.Errorf("knowledge is not available in the authorized search scope")
			}
		case dataAnalysisAuthorizationInternal:
			// The caller authorized this exact knowledge set before constructing the tool.
		default:
			return nil, fmt.Errorf("data analysis authorization context is not configured")
		}
	}
	loaded := make([]struct {
		knowledge *types.Knowledge
		schema    *TableSchema
	}, 0, len(knowledges))
	for _, knowledge := range knowledges {
		schema, err := t.loadFromKnowledge(ctx, knowledge)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, struct {
			knowledge *types.Knowledge
			schema    *TableSchema
		}{knowledge: knowledge, schema: schema})
	}
	if len(loaded) == 1 {
		return loaded[0].schema, nil
	}
	primaryName := loaded[0].schema.TableName
	combinedName := primaryName + "_combined"
	sourceFileColumn := availableMetadataColumn(loaded, dataSourceFileColumn)
	sourceKnowledgeColumn := availableMetadataColumn(loaded, dataSourceKnowledgeColumn)
	parts := make([]string, 0, len(loaded))
	for _, source := range loaded {
		parts = append(parts, fmt.Sprintf(
			"SELECT *, '%s' AS %s, '%s' AS %s FROM %s",
			sqlSingleQuoteEscape(source.knowledge.FileName), quoteDuckDBIdentifier(sourceFileColumn),
			sqlSingleQuoteEscape(source.knowledge.ID), quoteDuckDBIdentifier(sourceKnowledgeColumn),
			quoteDuckDBIdentifier(source.schema.TableName),
		))
	}
	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("start combining analysis sources: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "CREATE TABLE "+quoteDuckDBIdentifier(combinedName)+" AS "+strings.Join(parts, " UNION ALL BY NAME ")); err != nil {
		return nil, fmt.Errorf("combine analysis sources: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DROP TABLE "+quoteDuckDBIdentifier(primaryName)); err != nil {
		return nil, fmt.Errorf("replace primary analysis table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "ALTER TABLE "+quoteDuckDBIdentifier(combinedName)+" RENAME TO "+quoteDuckDBIdentifier(primaryName)); err != nil {
		return nil, fmt.Errorf("activate combined analysis table: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit combined analysis table: %w", err)
	}
	schema, err := t.profileAnalysisTable(ctx, primaryName)
	if err == nil {
		schema.Metadata["source_count"] = len(loaded)
		schema.Metadata["source_file_column"] = sourceFileColumn
		schema.Metadata["source_knowledge_column"] = sourceKnowledgeColumn
		t.loadedSchemas[knowledgeSchemaCacheKey(knowledges[0])] = schema
	}
	return schema, err
}

func availableMetadataColumn(loaded []struct {
	knowledge *types.Knowledge
	schema    *TableSchema
}, preferred string) string {
	used := make(map[string]bool)
	for _, source := range loaded {
		for _, column := range source.schema.Columns {
			used[strings.ToLower(column.Name)] = true
		}
	}
	name := preferred
	for suffix := 2; used[strings.ToLower(name)]; suffix++ {
		name = fmt.Sprintf("%s_%d", preferred, suffix)
	}
	return name
}

func (t *DataAnalysisTool) loadFromKnowledgeID(ctx context.Context, knowledgeID string) (*TableSchema, error) {
	// Unit-only callers may provide a preloaded schema without a knowledge
	// service. Production constructors always provide the service, so cached
	// production schemas still pass the visibility check below.
	if t.knowledgeService == nil {
		if schema := t.loadedSchemas[knowledgeID]; schema != nil {
			return schema, nil
		}
		return nil, fmt.Errorf("knowledge service is not configured")
	}
	// Use GetKnowledgeByIDOnly to support cross-tenant shared KB
	knowledge, err := t.knowledgeService.GetKnowledgeByIDOnly(ctx, knowledgeID)
	if err != nil {
		logger.Errorf(ctx, "[Tool][DataAnalysis] Failed to get knowledge by ID '%s': %v", knowledgeID, err)
		return nil, fmt.Errorf("failed to get knowledge by ID: %w", err)
	}
	switch t.authorization.mode {
	case dataAnalysisAuthorizationAgentScope:
		if !agentKnowledgeVisible(ctx, knowledge, t.authorization.searchTargets, t.authorization.governanceRepo) {
			return nil, fmt.Errorf("knowledge is not available in the authorized search scope")
		}
	case dataAnalysisAuthorizationInternal:
		// The caller authorized this exact knowledge before constructing the tool.
	default:
		return nil, fmt.Errorf("data analysis authorization context is not configured")
	}
	if schema := t.loadedSchemas[knowledgeSchemaCacheKey(knowledge)]; schema != nil {
		return schema, nil
	}

	return t.loadFromKnowledge(ctx, knowledge)
}

// LoadFromTable retrieves the schema information of an existing table
// Parameters:
//   - ctx: context for cancellation and timeout
//   - tableName: name of the table to query
//
// Returns:
//   - *TableSchema: schema information of the table
//   - error: any error that occurred during the operation
//
// Note: This function does NOT create the table, it only retrieves schema information
func (t *DataAnalysisTool) LoadFromTable(ctx context.Context, tableName string) (*TableSchema, error) {
	logger.Infof(ctx, "[Tool][DataAnalysis] Getting schema for table '%s' in session %s", tableName, t.sessionID)

	// Query to get column information using PRAGMA table_info or DESCRIBE
	schemaSQL := fmt.Sprintf("DESCRIBE \"%s\"", tableName)

	rows, err := t.db.QueryContext(ctx, schemaSQL)
	if err != nil {
		logger.Errorf(ctx, "[Tool][DataAnalysis] Failed to get table schema: %v", err)
		return nil, fmt.Errorf("failed to get table schema: %w", err)
	}
	defer rows.Close()

	// Parse column information
	columns := make([]ColumnInfo, 0)
	for rows.Next() {
		var colName, colType, nullable string
		var extra1, extra2, extra3 interface{} // DuckDB DESCRIBE may return additional columns

		// Try to scan with different column counts
		err := rows.Scan(&colName, &colType, &nullable, &extra1, &extra2, &extra3)
		if err != nil {
			// Try with fewer columns
			err = rows.Scan(&colName, &colType, &nullable)
			if err != nil {
				logger.Errorf(ctx, "[Tool][DataAnalysis] Failed to scan column info: %v", err)
				return nil, fmt.Errorf("failed to scan column info: %w", err)
			}
		}

		columns = append(columns, ColumnInfo{
			Name:     colName,
			Type:     colType,
			Nullable: nullable,
		})
	}

	if err := rows.Err(); err != nil {
		logger.Errorf(ctx, "[Tool][DataAnalysis] Error iterating schema rows: %v", err)
		return nil, fmt.Errorf("error iterating schema rows: %w", err)
	}

	// Get row count
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM \"%s\"", tableName)
	var rowCount int64
	if err := t.db.QueryRowContext(ctx, countSQL).Scan(&rowCount); err != nil {
		logger.Errorf(ctx, "[Tool][DataAnalysis] Failed to get row count: %v", err)
		return nil, fmt.Errorf("failed to get row count: %w", err)
	}

	schema := &TableSchema{
		TableName: tableName,
		Columns:   columns,
		RowCount:  rowCount,
		Metadata: map[string]interface{}{
			"column_count": len(columns),
			"session_id":   t.sessionID,
		},
	}

	logger.Infof(ctx, "[Tool][DataAnalysis] Retrieved schema for table '%s' in session %s: %d columns, %d rows",
		tableName, t.sessionID, len(columns), rowCount)

	return schema, nil
}

func (t *DataAnalysisTool) TableName(knowledge *types.Knowledge) string {
	knowledgePart := sanitizeDuckDBIdentifierPart(knowledge.ID)
	versionPart := ""
	if knowledge.CurrentVersionID != "" || !knowledge.UpdatedAt.IsZero() {
		versionDigest := sha256.Sum256([]byte(knowledgeSchemaCacheKey(knowledge)))
		versionPart = "_" + fmt.Sprintf("%x", versionDigest[:6])
	}
	t.identityOnce.Do(func() { t.instanceID = uuid.NewString() })
	sessionDigest := sha256.Sum256([]byte(t.sessionID + "|" + t.instanceID))
	sessionPart := fmt.Sprintf("%x", sessionDigest[:6])
	suffix := versionPart + "_" + sessionPart
	maxKnowledgeLength := maxSQLIdentifierLength - len("k_") - len(suffix)
	if len(knowledgePart) > maxKnowledgeLength {
		knowledgePart = knowledgePart[:maxKnowledgeLength]
	}
	return "k_" + knowledgePart + suffix
}

func knowledgeSchemaCacheKey(knowledge *types.Knowledge) string {
	if knowledge == nil {
		return ""
	}
	encoded, _ := json.Marshal([]interface{}{knowledge.TenantID, knowledge.ID, knowledge.CurrentVersionID, knowledge.UpdatedAt, knowledge.FilePath, knowledge.FileSize})
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

var nonDuckDBIdentifierPart = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

func sanitizeDuckDBIdentifierPart(value string) string {
	value = nonDuckDBIdentifierPart.ReplaceAllString(strings.TrimSpace(value), "_")
	value = strings.Trim(value, "_")
	if value == "" {
		return "unknown"
	}
	return value
}

// buildSchemaDescription builds a formatted schema description
func (t *TableSchema) Description() string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Table name: %s\n", t.TableName))
	builder.WriteString(fmt.Sprintf("Columns: %d\n", len(t.Columns)))
	builder.WriteString(fmt.Sprintf("Rows: %d\n\n", t.RowCount))
	builder.WriteString("Column info:\n")

	for _, col := range t.Columns {
		builder.WriteString(fmt.Sprintf("- %s (%s) %s\n", col.Name, col.Type, col.AnalysisType))
		if col.Multiline {
			builder.WriteString("  Contains multiline cells: a row may contain multiple independent values. Whole-cell negative matching can discard an entity that also has a valid matching value. Clarify row versus value exclusions; do not assume they are equivalent.\n")
		}
		if len(col.ValueExamples) > 0 {
			values, _ := json.Marshal(col.ValueExamples)
			builder.WriteString("  Observed value fragments (not exhaustive; never use sample frequency as a total): " + string(values) + "\n")
		}
	}

	return builder.String()
}

// resolveFileServiceForKnowledge resolves a provider-specific FileService based on the knowledge file path.
// It falls back to the injected default service when provider/config cannot be resolved.
func (t *DataAnalysisTool) resolveFileServiceForKnowledge(ctx context.Context, knowledge *types.Knowledge) interfaces.FileService {
	if knowledge == nil {
		logger.Warnf(ctx, "[Tool][DataAnalysis][storage] fallback default: session_id=%s reason=knowledge_nil", t.sessionID)
		return t.fileService
	}

	kbID := strings.TrimSpace(knowledge.KnowledgeBaseID)
	var kb *types.KnowledgeBase
	if t.knowledgeBaseService != nil && kbID != "" {
		var err error
		kb, err = t.knowledgeBaseService.GetKnowledgeBaseByID(ctx, kbID)
		if err != nil {
			logger.Warnf(ctx, "[Tool][DataAnalysis][storage] get kb failed, fallback default: session_id=%s knowledge_id=%s kb_id=%s err=%v",
				t.sessionID, knowledge.ID, kbID, err)
			return t.fileService
		}
	}
	if kb == nil && kbID != "" {
		logger.Infof(ctx, "[Tool][DataAnalysis][storage] kb not found, fallback default: session_id=%s knowledge_id=%s kb_id=%s",
			t.sessionID, knowledge.ID, kbID)
		return t.fileService
	}

	provider := types.InferStorageFromFilePath(knowledge.FilePath)
	tenant, _ := ctx.Value(types.TenantInfoContextKey).(*types.Tenant)
	if tenant == nil {
		tenantID := uint64(0)
		if tid, ok := ctx.Value(types.TenantIDContextKey).(uint64); ok {
			tenantID = tid
		}
		if tenantID == 0 && kb != nil {
			tenantID = knowledge.TenantID
		}
		if tenantID > 0 && t.tenantService != nil {
			resolvedTenant, err := t.tenantService.GetTenantByID(ctx, tenantID)
			if err != nil {
				logger.Warnf(ctx, "[Tool][DataAnalysis][storage] get tenant failed: session_id=%s knowledge_id=%s kb_id=%s tenant_id=%d err=%v",
					t.sessionID, knowledge.ID, kbID, tenantID, err)
			} else if resolvedTenant != nil {
				tenant = resolvedTenant
				logger.Infof(ctx, "[Tool][DataAnalysis][storage] resolved tenant from service: session_id=%s knowledge_id=%s kb_id=%s tenant_id=%d",
					t.sessionID, knowledge.ID, kbID, tenantID)
			}
		}
	}
	if provider == "" && tenant != nil && tenant.StorageEngineConfig != nil {
		provider = strings.ToLower(strings.TrimSpace(tenant.StorageEngineConfig.DefaultProvider))
	}

	if provider == "" || tenant == nil || tenant.StorageEngineConfig == nil {
		hasTenantStorageConfig := tenant != nil && tenant.StorageEngineConfig != nil
		logger.Infof(ctx, "[Tool][DataAnalysis][storage] fallback default: session_id=%s knowledge_id=%s kb_id=%s provider=%q tenant_cfg=%t",
			t.sessionID, knowledge.ID, kbID, provider, hasTenantStorageConfig)
		return t.fileService
	}

	storageConfig := tenant.StorageEngineConfig
	baseDir := strings.TrimSpace(os.Getenv("LOCAL_STORAGE_BASE_DIR"))

	resolvedSvc, resolvedProvider, err := filesvc.NewFileServiceFromStorageConfig(provider, storageConfig, baseDir)
	if err != nil {
		logger.Warnf(ctx, "[Tool][DataAnalysis][storage] create file service failed, fallback default: session_id=%s knowledge_id=%s kb_id=%s provider=%s err=%v",
			t.sessionID, knowledge.ID, kbID, provider, err)
		return t.fileService
	}

	logger.Infof(ctx, "[Tool][DataAnalysis][storage] resolved file service: session_id=%s knowledge_id=%s kb_id=%s provider=%s",
		t.sessionID, knowledge.ID, kbID, resolvedProvider)
	return resolvedSvc
}

func (t *DataAnalysisTool) hasCreatedTable(tableName string) bool {
	for _, name := range t.createdTables {
		if name == tableName {
			return true
		}
	}
	return false
}
