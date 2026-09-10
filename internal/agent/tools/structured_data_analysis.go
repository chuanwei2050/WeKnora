package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/structuredquery"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
)

type StructuredDataAnalysisInput struct {
	KnowledgeID string `json:"knowledge_id" jsonschema:"id of the tabular knowledge to query"`
	Question    string `json:"question" jsonschema:"natural-language question to answer from the complete table"`
}

type StructuredDataAnalysisTool struct {
	BaseTool
	knowledgeService interfaces.KnowledgeService
	searchTargets    types.SearchTargets
	governanceRepo   interfaces.KnowledgeGovernanceRepository
	client           structuredquery.Client
}

func NewStructuredDataAnalysisTool(cfg *config.Config, knowledgeService interfaces.KnowledgeService, searchTargets types.SearchTargets, governanceRepo interfaces.KnowledgeGovernanceRepository) *StructuredDataAnalysisTool {
	client := structuredquery.Client{}
	if cfg != nil && cfg.StructuredQuery != nil {
		client.BaseURL = cfg.StructuredQuery.BaseURL
		client.APIKey = cfg.StructuredQuery.APIKey
		client.Timeout = time.Duration(cfg.StructuredQuery.RequestTimeout) * time.Second
	}
	return &StructuredDataAnalysisTool{
		BaseTool:         NewBaseTool(ToolDataAnalysis, "Query a complete CSV or Excel dataset using a natural-language question. SQL generation and safe execution are owned by the structured-query service.", utils.GenerateSchema[StructuredDataAnalysisInput]()),
		knowledgeService: knowledgeService,
		searchTargets:    searchTargets,
		governanceRepo:   governanceRepo,
		client:           client,
	}
}

func (t *StructuredDataAnalysisTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var input StructuredDataAnalysisInput
	if err := json.Unmarshal(args, &input); err != nil {
		return &types.ToolResult{Success: false, Error: "invalid structured data analysis input"}, err
	}
	input.KnowledgeID = strings.TrimSpace(input.KnowledgeID)
	input.Question = strings.TrimSpace(input.Question)
	if input.KnowledgeID == "" || input.Question == "" || t.client.BaseURL == "" || t.client.APIKey == "" {
		return &types.ToolResult{Success: false, Error: "structured query is unavailable or input is incomplete"}, fmt.Errorf("structured query unavailable")
	}
	knowledge, err := t.knowledgeService.GetKnowledgeByIDOnly(ctx, input.KnowledgeID)
	if err != nil || knowledge == nil || knowledge.EnableStatus != "enabled" || knowledge.ParseStatus != types.ParseStatusCompleted || !agentKnowledgeVisible(ctx, knowledge, t.searchTargets, t.governanceRepo) {
		return &types.ToolResult{Success: false, Error: "knowledge is not available in the authorized search scope"}, fmt.Errorf("knowledge is not available")
	}
	datasetID := knowledge.GetMetadata()["structured_dataset_id"]
	if datasetID == "" {
		return &types.ToolResult{Success: false, Error: "structured dataset is not ready"}, fmt.Errorf("structured dataset is not ready")
	}
	response, err := t.client.Query(ctx, knowledge.TenantID, structuredquery.Request{
		Namespace: knowledge.KnowledgeBaseID, Question: input.Question, DatasetIDs: []string{datasetID},
	})
	if err != nil {
		return &types.ToolResult{Success: false, Error: "structured query failed"}, err
	}
	if response.Route == "none" {
		return &types.ToolResult{Success: true, Output: "The question does not require a structured query.", Data: map[string]interface{}{"route": "none"}}, nil
	}
	payload, _ := json.Marshal(map[string]interface{}{"columns": response.Columns, "rows": response.Rows, "sources": response.Sources})
	return &types.ToolResult{Success: true, Output: string(payload), Data: map[string]interface{}{
		"route": response.Route, "query": response.SQL, "columns": response.Columns, "rows": response.Rows,
		"model_calls": response.ModelCalls, "timings": response.Timings, "sources": response.Sources,
	}}, nil
}
