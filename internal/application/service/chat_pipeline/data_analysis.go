package chatpipeline

import (
	"context"
	"database/sql"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const dataAnalysisEvidenceInstruction = "结构化查询结果：这是对原始表格执行 SQL 得到的一条候选证据。请将其与 ES、向量检索的局部检索证据共同判断，不要机械地优先采用任一来源。检索证据由 rerank 决定相关性、候选资格和最终顺序。检索证据入选只表示相关，不表示覆盖完整；判断时核对每条证据的查询条件、语义匹配程度和数据覆盖范围。涉及完整名单、总数或聚合时，只有过滤条件覆盖目标字段和值的结构化结果才能证明完整性；若证据冲突，请指出冲突及采用结论的理由。"

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
