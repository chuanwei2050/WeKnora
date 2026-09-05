package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"gorm.io/gorm"
)

var databaseQueryTool = BaseTool{
	name: ToolDatabaseQuery,
	description: `Execute SQL queries to retrieve information from the database.

## Security Features
- Automatic tenant_id injection: All queries are automatically filtered by the logged-in user's tenant_id
- Automatic soft-delete filtering: All queries are automatically filtered to include only records with deleted_at IS NULL
- Read-only queries: Only SELECT statements are allowed
- Safe tables: Only allow queries on authorized tables (knowledge_bases, knowledges, chunks)

## Available Tables and Columns

### knowledge_bases
- id (VARCHAR): Knowledge base ID
- name (VARCHAR): Knowledge base name
- description (TEXT): Description
- tenant_id (INTEGER): Owner tenant ID
- embedding_model_id, summary_model_id, rerank_model_id (VARCHAR): Model IDs
- vlm_config (JSON): Includes VLM settings such as enabled flag and model_id
- created_at, updated_at, deleted_at (TIMESTAMP)

### knowledges (documents)
- id (VARCHAR): Document ID
- tenant_id (INTEGER): Owner tenant ID
- knowledge_base_id (VARCHAR): Parent knowledge base ID
- type (VARCHAR): Document type
- title (VARCHAR): Document title
- description (TEXT): Description
- source (VARCHAR): Source location
- parse_status (VARCHAR): Processing status (unprocessed/processing/completed/failed)
- enable_status (VARCHAR): Enable status (enabled/disabled)
- file_name, file_type (VARCHAR): File information
- file_size, storage_size (BIGINT): Size in bytes
- created_at, updated_at, processed_at, deleted_at (TIMESTAMP)



### chunks
- id (VARCHAR): Chunk ID
- tenant_id (INTEGER): Owner tenant ID
- knowledge_base_id (VARCHAR): Parent knowledge base ID
- knowledge_id (VARCHAR): Parent document ID
- content (TEXT): Chunk content
- chunk_index (INTEGER): Index in document
- is_enabled (BOOLEAN): Enable status
- chunk_type (VARCHAR): Type (text/image/table)
- created_at, updated_at, deleted_at (TIMESTAMP)

## Usage Examples

Query knowledge base information:
{
  "sql": "SELECT id, name, description FROM knowledge_bases ORDER BY created_at DESC LIMIT 10"
}

Count documents by status:
{
  "sql": "SELECT parse_status, COUNT(*) as count FROM knowledges GROUP BY parse_status"
}

Find recent documents:
{
  "sql": "SELECT id, title, created_at FROM knowledges ORDER BY created_at DESC LIMIT 5"
}

Get storage usage:
{
  "sql": "SELECT SUM(storage_size) as total_storage FROM knowledges"
}

Join knowledge bases and documents:
{
  "sql": "SELECT kb.name as kb_name, COUNT(k.id) as doc_count FROM knowledge_bases kb LEFT JOIN knowledges k ON kb.id = k.knowledge_base_id GROUP BY kb.id, kb.name"
}

## Important Notes
- DO NOT include tenant_id in WHERE clause - it's automatically added
- DO NOT include deleted_at filtering manually unless needed - default query already enforces deleted_at IS NULL
- Only SELECT queries are allowed
- Limit results with LIMIT clause for better performance
- Use appropriate JOINs when querying across tables
- All timestamps are in UTC with time zone`,
	schema: utils.GenerateSchema[DatabaseQueryInput](),
}

type DatabaseQueryInput struct {
	SQL string `json:"sql" jsonschema:"The SELECT SQL query to execute. DO NOT include tenant_id condition - it will be automatically added for security."`
}

// DatabaseQueryTool allows AI to query the database with auto-injected tenant_id for security
type DatabaseQueryTool struct {
	BaseTool
	db             *gorm.DB
	searchTargets  types.SearchTargets
	governanceRepo interfaces.KnowledgeGovernanceRepository
}

// NewDatabaseQueryTool creates a new database query tool
func NewDatabaseQueryTool(db *gorm.DB, searchTargets types.SearchTargets) *DatabaseQueryTool {
	return NewDatabaseQueryToolWithGovernance(db, searchTargets, nil)
}

func NewDatabaseQueryToolWithGovernance(
	db *gorm.DB,
	searchTargets types.SearchTargets,
	governanceRepo interfaces.KnowledgeGovernanceRepository,
) *DatabaseQueryTool {
	return &DatabaseQueryTool{
		BaseTool:       databaseQueryTool,
		db:             db,
		searchTargets:  searchTargets,
		governanceRepo: governanceRepo,
	}
}

// Execute executes the database query tool
func (t *DatabaseQueryTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	logger.Infof(ctx, "[Tool][DatabaseQuery] Execute started")
	ctx, cancel := context.WithTimeout(ctx, dataAnalysisQueryTimeout)
	defer cancel()

	tenantID := uint64(0)
	if tid, ok := ctx.Value(types.TenantIDContextKey).(uint64); ok {
		tenantID = tid
	}

	// Parse args from json.RawMessage
	var input DatabaseQueryInput
	if err := json.Unmarshal(args, &input); err != nil {
		logger.Errorf(ctx, "[Tool][DatabaseQuery] Failed to parse args: %v", err)
		return &types.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to parse args: %v", err),
		}, err
	}

	// Extract SQL from input
	if input.SQL == "" {
		logger.Errorf(ctx, "[Tool][DatabaseQuery] Missing or invalid SQL parameter")
		return &types.ToolResult{
			Success: false,
			Error:   "Missing or invalid 'sql' parameter",
		}, fmt.Errorf("missing sql parameter")
	}

	logger.Infof(ctx, "[Tool][DatabaseQuery] Original SQL query:\n%s", input.SQL)
	logger.Infof(ctx, "[Tool][DatabaseQuery] Tenant ID: %d", tenantID)

	// Validate and secure the SQL query
	logger.Debugf(ctx, "[Tool][DatabaseQuery] Validating and securing SQL...")
	securedSQL, err := t.validateAndSecureSQL(ctx, input.SQL, tenantID)
	if err != nil {
		logger.Errorf(ctx, "[Tool][DatabaseQuery] SQL validation failed: %v", err)
		return &types.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("SQL validation failed: %v", err),
		}, err
	}

	logger.Infof(ctx, "[Tool][DatabaseQuery] Secured SQL query:\n%s", securedSQL)
	logger.Infof(ctx, "Executing secured SQL query - original: %s, secured: %s, tenant_id: %d",
		input.SQL, securedSQL, tenantID)

	securedSQL = fmt.Sprintf("SELECT * FROM (%s) AS bounded_query LIMIT %d", strings.TrimSuffix(strings.TrimSpace(securedSQL), ";"), dataAnalysisDefaultRows+1)
	// Execute the query
	logger.Infof(ctx, "[Tool][DatabaseQuery] Executing query against database...")
	rows, err := t.db.WithContext(ctx).Raw(securedSQL).Rows()
	if err != nil {
		logger.Errorf(ctx, "[Tool][DatabaseQuery] Query execution failed: %v", err)
		return &types.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Query execution failed: %v", err),
		}, err
	}
	defer rows.Close()

	logger.Debugf(ctx, "[Tool][DatabaseQuery] Query executed successfully, processing rows...")

	columns, results, truncated, err := scanSQLRows(rows, dataAnalysisDefaultRows, dataAnalysisMaxResultBytes)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, err
	}

	logger.Infof(ctx, "[Tool][DatabaseQuery] Retrieved %d rows with %d columns", len(results), len(columns))
	logger.Debugf(ctx, "[Tool][DatabaseQuery] Columns: %v", columns)

	// Log first few rows for debugging
	if len(results) > 0 {
		logger.Debugf(ctx, "[Tool][DatabaseQuery] First row sample:")
		for key, value := range results[0] {
			logger.Debugf(ctx, "[Tool][DatabaseQuery]   %s: %v", key, value)
		}
	}

	// Format output
	logger.Debugf(ctx, "[Tool][DatabaseQuery] Formatting query results...")
	output := t.formatQueryResults(columns, results)
	if truncated {
		if len(results) == 0 {
			output = strings.ReplaceAll(output, "No matching records found.", "Matching records exceeded the output budget.")
		}
		output += "\n结果已截断，当前展示不是完整清单。请分页获取明细；仅在需要统计总数时使用聚合查询。\n"
	}

	logger.Infof(ctx, "[Tool][DatabaseQuery] Execute completed successfully: %d rows returned", len(results))
	return &types.ToolResult{
		Success: true,
		Output:  output,
		Data: map[string]interface{}{
			"columns":      columns,
			"rows":         results,
			"row_count":    len(results),
			"truncated":    truncated,
			"display_type": "database_query",
		},
	}, nil
}

// validateAndSecureSQL validates the SQL query and injects tenant_id conditions
func (t *DatabaseQueryTool) validateAndSecureSQL(ctx context.Context, sqlQuery string, tenantID uint64) (string, error) {
	if tenantID == 0 {
		return "", fmt.Errorf("tenant is required")
	}
	kbIDs := t.searchTargets.GetAllKnowledgeBaseIDs()
	if t.searchTargets != nil && len(kbIDs) == 0 {
		kbIDs = []string{noVisibleKnowledgeID}
	}
	var knowledgeIDs []string
	var currentVersions map[string]string
	if t.searchTargets != nil {
		visibleIDs, versions, err := t.visibleKnowledgeVersions(ctx)
		if err != nil {
			return "", err
		}
		knowledgeIDs = visibleIDs
		currentVersions = versions
	}

	options := []utils.SQLValidationOption{
		utils.WithSecurityDefaults(tenantID),
		utils.WithSoftDeleteFilter("knowledge_bases", "knowledges", "chunks"),
		utils.WithHiddenKBFilter(),
		utils.WithInjectionRiskCheck(),
		utils.WithSearchScopeFilter(kbIDs, knowledgeIDs),
	}
	if t.searchTargets != nil {
		options = append(options, utils.WithKnowledgeVersionFilter(currentVersions))
	}
	securedSQL, validationResult, err := utils.ValidateAndSecureSQL(sqlQuery, options...)
	if err != nil {
		return "", err
	}

	if !validationResult.Valid {
		var errMsgs []string
		for _, valErr := range validationResult.Errors {
			errMsgs = append(errMsgs, fmt.Sprintf("%s: %s", valErr.Type, valErr.Message))
		}
		return "", fmt.Errorf("validation failed: %s", strings.Join(errMsgs, "; "))
	}

	return securedSQL, nil
}

const noVisibleKnowledgeID = "__weknora_no_visible_knowledge__"

func (t *DatabaseQueryTool) visibleKnowledgeVersions(ctx context.Context) ([]string, map[string]string, error) {
	type knowledgeScopeRow struct {
		ID               string `gorm:"column:id"`
		KnowledgeBaseID  string `gorm:"column:knowledge_base_id"`
		TenantID         uint64 `gorm:"column:tenant_id"`
		CurrentVersionID string `gorm:"column:current_version_id"`
		PendingVersionID string `gorm:"column:pending_version_id"`
	}
	var rows []knowledgeScopeRow
	query := t.db.WithContext(ctx).Table("knowledges").Select("id, knowledge_base_id, tenant_id, current_version_id, pending_version_id").Where("deleted_at IS NULL")
	if len(t.searchTargets) > 0 {
		conditions := make([]string, 0, len(t.searchTargets))
		args := make([]interface{}, 0, len(t.searchTargets)*2)
		for _, target := range t.searchTargets {
			if target == nil || target.KnowledgeBaseID == "" || target.TenantID == 0 {
				continue
			}
			condition := "(knowledge_base_id = ? AND tenant_id = ?"
			args = append(args, target.KnowledgeBaseID, target.TenantID)
			if target.Type == types.SearchTargetTypeKnowledge && target.KnowledgeIDs != nil {
				if len(target.KnowledgeIDs) == 0 {
					condition += " AND 1 = 0"
				} else {
					condition += " AND id IN ?"
					args = append(args, target.KnowledgeIDs)
				}
			}
			if target.TagIDs != nil {
				if len(target.TagIDs) == 0 {
					condition += " AND 1 = 0"
				} else {
					condition += " AND tag_id IN ?"
					args = append(args, target.TagIDs)
				}
			}
			conditions = append(conditions, condition+")")
		}
		if len(conditions) == 0 {
			return []string{noVisibleKnowledgeID}, map[string]string{noVisibleKnowledgeID: ""}, nil
		}
		query = query.Where("("+strings.Join(conditions, " OR ")+")", args...)
	}
	// Keep visibility checks in one database round-trip instead of fetching a
	// governance record separately for every document.
	query = query.Where("coalesce(pending_version_id, '') = ''")
	if t.governanceRepo == nil {
		query = query.Where("coalesce(current_version_id, '') = ''")
	} else {
		now := time.Now().UTC()
		query = query.Where(`((coalesce(current_version_id, '') = '' AND NOT EXISTS
   (SELECT 1 FROM knowledge_versions v WHERE v.knowledge_id = knowledges.id AND v.tenant_id = knowledges.tenant_id))
   OR EXISTS (SELECT 1 FROM knowledge_versions v WHERE v.id = knowledges.current_version_id
   AND v.knowledge_id = knowledges.id AND v.tenant_id = knowledges.tenant_id AND v.status = ?
   AND (v.effective_at IS NULL OR v.effective_at <= ?) AND (v.expires_at IS NULL OR v.expires_at > ?)))`, types.KnowledgeVersionActive, now, now)
	}
	if len(t.searchTargets) == 0 {
		return []string{noVisibleKnowledgeID}, map[string]string{noVisibleKnowledgeID: ""}, nil
	}
	if err := query.Find(&rows).Error; err != nil {
		return nil, nil, fmt.Errorf("load authorized knowledge scope: %w", err)
	}
	visible := make([]string, 0, len(rows))
	currentVersions := make(map[string]string, len(rows))
	for _, row := range rows {
		knowledge := &types.Knowledge{ID: row.ID, TenantID: row.TenantID, KnowledgeBaseID: row.KnowledgeBaseID, CurrentVersionID: row.CurrentVersionID, PendingVersionID: row.PendingVersionID}
		if knowledgeInAgentSearchScope(knowledge, t.searchTargets) {
			visible = append(visible, row.ID)
			currentVersions[row.ID] = row.CurrentVersionID
		}
	}
	if len(visible) == 0 {
		return []string{noVisibleKnowledgeID}, map[string]string{noVisibleKnowledgeID: ""}, nil
	}
	return visible, currentVersions, nil
}

// formatQueryResults formats query results into readable text
func (t *DatabaseQueryTool) formatQueryResults(
	columns []string,
	results []map[string]interface{},
) string {
	var output strings.Builder
	output.WriteString("=== Query Results ===\n\n")
	output.WriteString(fmt.Sprintf("Returned %d rows\n\n", len(results)))

	if len(results) == 0 {
		output.WriteString("No matching records found.\n")
		return output.String()
	}

	output.WriteString("=== Data Details ===\n\n")

	// Format each row
	for i, row := range results {
		output.WriteString(fmt.Sprintf("--- Record #%d ---\n", i+1))
		for _, col := range columns {
			value := row[col]
			// Format the value
			var formattedValue string
			if value == nil {
				formattedValue = "<NULL>"
			} else if jsonData, err := json.Marshal(value); err == nil {
				// Check if it's a complex type
				switch v := value.(type) {
				case string:
					formattedValue = v
				case []byte:
					formattedValue = string(v)
				default:
					formattedValue = string(jsonData)
				}
			} else {
				formattedValue = fmt.Sprintf("%v", value)
			}

			output.WriteString(fmt.Sprintf("  %s: %s\n", col, formattedValue))
		}
		output.WriteString("\n")
	}

	// Add summary statistics if applicable
	if len(results) > 10 {
		output.WriteString(fmt.Sprintf("Showing %d returned records. Check the truncation flag before treating these as complete.\n", len(results)))
	}

	return output.String()
}
