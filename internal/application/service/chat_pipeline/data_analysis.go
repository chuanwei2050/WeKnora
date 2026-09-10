package chatpipeline

import (
	"context"
	"database/sql"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const dataAnalysisEvidenceInstruction = "结构化查询结果：请与其他检索证据交叉核对后回答。统计、聚合、排序和计算类问题以 SQL 结果为准；普通事实和列举类问题不得因 SQL 未命中而忽略其他证据。补充列举时保留证据中的实际表述，不得臆造同义或纳入无关相邻记录。"

type PluginDataAnalysis struct {
	knowledgeService interfaces.KnowledgeService
	config           *config.Config
}

func NewPluginDataAnalysis(
	eventManager *EventManager,
	_ interfaces.ModelService,
	_ interfaces.KnowledgeBaseService,
	knowledgeService interfaces.KnowledgeService,
	_ interfaces.FileService,
	_ interfaces.ChunkRepository,
	_ interfaces.TenantService,
	_ interfaces.KnowledgeGovernanceRepository,
	_ interfaces.KnowledgeTableSchemaRepository,
	_ *sql.DB,
	appConfig *config.Config,
) *PluginDataAnalysis {
	plugin := &PluginDataAnalysis{knowledgeService: knowledgeService, config: appConfig}
	eventManager.Register(plugin)
	return plugin
}

func (p *PluginDataAnalysis) ActivationEvents() []types.EventType {
	return []types.EventType{types.STRUCTURED_QUERY_START, types.DATA_ANALYSIS}
}

func (p *PluginDataAnalysis) OnEvent(ctx context.Context, eventType types.EventType, manage *types.ChatManage, next func() *PluginError) *PluginError {
	if eventType == types.STRUCTURED_QUERY_START {
		if p.structuredQueryEnabled() {
			// Start before query understanding. The structured service owns the
			// sql/none decision and runs concurrently with all RAG stages.
			manage.StructuredQueryDone = p.startStructuredQuery(ctx, manage)
		}
		return next()
	}
	if !p.structuredQueryEnabled() {
		if eventType == types.DATA_ANALYSIS && manage.RerankOutcome == types.RerankOutcomeNoRelevantResult && len(manage.MergeResult) == 0 {
			return ErrSearchNothing
		}
		return next()
	}
	if manage.StructuredQueryDone != nil {
		manage.DataAnalysisResult = <-manage.StructuredQueryDone
		manage.DataAnalysisAttempted = true
		if len(manage.DataAnalysisResult) > 0 {
			stageID, stageStarted := emitPipelineStageStart(ctx, manage, "data_analysis", "表格分析")
			// Keep the reranked evidence beside the complete-table SQL result. The
			// answer model can then identify wording variants in the target field
			// without treating every person in a surrounding chunk as a match.
			manage.MergeResult = append(manage.MergeResult, manage.DataAnalysisResult...)
			emitPipelineStageResult(ctx, manage, stageID, "data_analysis", "表格分析完成，已交叉核对结构化结果和检索证据", stageStarted, true, map[string]interface{}{
				"status": "completed", "structured_result_count": len(manage.DataAnalysisResult),
			})
		}
	}
	if len(manage.MergeResult) == 0 {
		return ErrSearchNothing
	}
	return next()
}
