package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/google/uuid"
)

const (
	defaultTableAnalysisMaxRows    = 20
	maximumTableAnalysisMaxRows    = 50
	maximumTableAnalysisQueryChars = 4000
)

var trailingTableAnalysisLimit = regexp.MustCompile(`(?is)\s+limit\s+\d+(?:\s+offset\s+\d+)?\s*$`)

type tableAnalysisPlan struct {
	Queries []tableAnalysisPlanItem `json:"queries" jsonschema:"one SQL plan for every supplied query ID"`
}

type tableAnalysisPlanItem struct {
	ID  string `json:"id" jsonschema:"exact supplied query ID"`
	SQL string `json:"sql" jsonschema:"one read-only SELECT using table_schema.table_name exactly as the table name"`
}

func (s *sessionService) AnalyzeKnowledgeTable(
	ctx context.Context,
	knowledgeID string,
	queries []types.TableAnalysisQuery,
	maxRows int,
	modelID string,
) (*types.TableAnalysisResult, error) {
	knowledgeID = strings.TrimSpace(knowledgeID)
	if knowledgeID == "" || len(queries) == 0 || len(queries) > 100 {
		return nil, fmt.Errorf("structured table analysis requires one knowledge ID and 1-100 queries")
	}
	if maxRows <= 0 {
		maxRows = defaultTableAnalysisMaxRows
	}
	if maxRows > maximumTableAnalysisMaxRows {
		return nil, fmt.Errorf("structured table analysis max_rows exceeds %d", maximumTableAnalysisMaxRows)
	}
	seenIDs := make(map[string]bool, len(queries))
	for _, query := range queries {
		if strings.TrimSpace(query.ID) == "" || strings.TrimSpace(query.Query) == "" || seenIDs[query.ID] {
			return nil, fmt.Errorf("structured table analysis queries require unique non-empty IDs and text")
		}
		if utf8.RuneCountInString(query.Query) > maximumTableAnalysisQueryChars {
			return nil, fmt.Errorf("structured table analysis query exceeds %d characters", maximumTableAnalysisQueryChars)
		}
		seenIDs[query.ID] = true
	}

	knowledge, err := s.knowledgeService.GetKnowledgeByID(ctx, knowledgeID)
	if err != nil {
		return nil, fmt.Errorf("structured table knowledge is unavailable: %w", err)
	}
	fileType := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(knowledge.FileType), "."))
	if fileType != "xlsx" && fileType != "xls" && fileType != "csv" {
		return nil, fmt.Errorf("structured table analysis only supports xlsx, xls, and csv")
	}
	if knowledge.EnableStatus != "" && knowledge.EnableStatus != "enabled" {
		return nil, fmt.Errorf("structured table knowledge is not enabled")
	}
	if knowledge.ParseStatus != "" && knowledge.ParseStatus != "completed" {
		return nil, fmt.Errorf("structured table knowledge is not parsed")
	}

	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		kb, kbErr := s.knowledgeBaseService.GetKnowledgeBaseByID(ctx, knowledge.KnowledgeBaseID)
		if kbErr != nil || kb == nil || kb.SummaryModelID == "" {
			return nil, fmt.Errorf("structured table knowledge base lacks an analysis model")
		}
		modelID = kb.SummaryModelID
	}
	model, err := s.modelService.GetChatModel(ctx, modelID)
	if err != nil {
		return nil, fmt.Errorf("failed to load structured table analysis model: %w", err)
	}

	sessionID := "business_table_analysis_" + uuid.NewString()
	tool := tools.NewDataAnalysisTool(
		s.knowledgeBaseService,
		s.knowledgeService,
		s.tenantService,
		s.fileService,
		s.duckDB,
		sessionID,
	)
	defer tool.Cleanup(ctx)
	schema, err := tool.LoadFromKnowledge(ctx, knowledge)
	if err != nil {
		return nil, fmt.Errorf("failed to load structured table: %w", err)
	}

	plan, err := planTableAnalysisQueries(ctx, model, knowledgeID, queries, schema)
	if err != nil {
		return nil, err
	}
	results := make([]types.TableAnalysisQueryResult, 0, len(queries))
	for _, query := range queries {
		sqlText := plan[query.ID]
		boundedSQL, boundErr := boundTableAnalysisSQL(sqlText, maxRows)
		if boundErr != nil {
			results = append(results, types.TableAnalysisQueryResult{
				ID: query.ID, Status: "failed", Rows: []map[string]string{}, Error: boundErr.Error(),
			})
			continue
		}
		args, _ := json.Marshal(tools.DataAnalysisInput{KnowledgeID: knowledgeID, Sql: boundedSQL})
		toolResult, executeErr := tool.Execute(ctx, args)
		if executeErr != nil || toolResult == nil || !toolResult.Success {
			errorText := "structured table query failed"
			if toolResult != nil && toolResult.Error != "" {
				errorText = toolResult.Error
			}
			results = append(results, types.TableAnalysisQueryResult{
				ID: query.ID, Status: "failed", Rows: []map[string]string{}, Error: errorText,
			})
			continue
		}
		rows, _ := toolResult.Data["rows"].([]map[string]string)
		auditSQL, _ := toolResult.Data["query"].(string)
		status := "matched"
		if len(rows) == 0 {
			status = "not_found"
		}
		results = append(results, types.TableAnalysisQueryResult{
			ID: query.ID, Status: status, Rows: rows, RowCount: len(rows), SQL: auditSQL,
		})
	}
	return &types.TableAnalysisResult{
		KnowledgeID:     knowledge.ID,
		KnowledgeBaseID: knowledge.KnowledgeBaseID,
		FileType:        fileType,
		Filename:        knowledge.FileName,
		Results:         results,
	}, nil
}

func planTableAnalysisQueries(
	ctx context.Context,
	model chat.Chat,
	knowledgeID string,
	queries []types.TableAnalysisQuery,
	schema *tools.TableSchema,
) (map[string]string, error) {
	format := utils.GenerateSchema[tableAnalysisPlan]()
	response, err := model.Chat(ctx, []chat.Message{{
		Role:    "user",
		Content: tableAnalysisPlanningPrompt(knowledgeID, queries, schema),
	}}, &chat.ChatOptions{Temperature: 0.1, Format: format})
	if err != nil {
		return nil, fmt.Errorf("failed to plan structured table queries: %w", err)
	}
	var raw tableAnalysisPlan
	if err := json.Unmarshal([]byte(response.Content), &raw); err != nil {
		return nil, fmt.Errorf("structured table query plan is invalid JSON: %w", err)
	}
	plans := make(map[string]string, len(raw.Queries))
	for _, item := range raw.Queries {
		if !seenQueryID(queries, item.ID) || strings.TrimSpace(item.SQL) == "" || plans[item.ID] != "" {
			return nil, fmt.Errorf("structured table query plan contains unknown, duplicate, or empty entries")
		}
		plans[item.ID] = item.SQL
	}
	if len(plans) != len(queries) {
		return nil, fmt.Errorf("structured table query plan omitted required query IDs")
	}
	return plans, nil
}

func tableAnalysisPlanningPrompt(
	knowledgeID string,
	queries []types.TableAnalysisQuery,
	schema *tools.TableSchema,
) string {
	payload, _ := json.Marshal(map[string]interface{}{
		"knowledge_id": knowledgeID,
		"table_schema": schema,
		"queries":      queries,
	})
	return "Generate exactly one read-only DuckDB SELECT for every supplied query ID. " +
		"Use table_schema.table_name exactly as the only table name. The knowledge_id is authorization context and must not appear as a SQL table name. " +
		"Select only rows and columns that directly answer the business query. Include __sheet_name when available. " +
		"Do not use external functions, files, URLs, DDL, DML, PRAGMA, SHOW, DESCRIBE, EXPLAIN, or multiple statements. " +
		"Do not omit or invent query IDs. Input: " + string(payload)
}

func seenQueryID(queries []types.TableAnalysisQuery, id string) bool {
	for _, query := range queries {
		if query.ID == id {
			return true
		}
	}
	return false
}

func boundTableAnalysisSQL(value string, maxRows int) (string, error) {
	query := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), ";"))
	if !strings.HasPrefix(strings.ToLower(query), "select") {
		return "", fmt.Errorf("structured table query must be a read-only SELECT")
	}
	if maxRows <= 0 || maxRows > maximumTableAnalysisMaxRows {
		return "", fmt.Errorf("structured table query row limit is invalid")
	}
	query = strings.TrimSpace(trailingTableAnalysisLimit.ReplaceAllString(query, ""))
	return fmt.Sprintf("%s LIMIT %d", query, maxRows), nil
}
