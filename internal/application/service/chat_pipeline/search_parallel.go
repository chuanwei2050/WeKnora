package chatpipeline

import (
	"context"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// PluginSearchParallel implements parallel search functionality combining chunk search and entity search
type PluginSearchParallel struct {
	// Chunk search dependencies
	knowledgeBaseService interfaces.KnowledgeBaseService
	knowledgeService     interfaces.KnowledgeService
	config               *config.Config
	webSearchService     interfaces.WebSearchService
	tenantService        interfaces.TenantService
	sessionService       interfaces.SessionService

	// Entity search dependencies
	graphRepo     interfaces.RetrieveGraphRepository
	chunkRepo     interfaces.ChunkRepository
	knowledgeRepo interfaces.KnowledgeRepository

	// Internal plugins
	searchPlugin       *PluginSearch
	searchEntityPlugin *PluginSearchEntity
}

// NewPluginSearchParallel creates a new parallel search plugin
func NewPluginSearchParallel(
	eventManager *EventManager,
	knowledgeBaseService interfaces.KnowledgeBaseService,
	knowledgeService interfaces.KnowledgeService,
	chunkService interfaces.ChunkService,
	config *config.Config,
	webSearchService interfaces.WebSearchService,
	tenantService interfaces.TenantService,
	sessionService interfaces.SessionService,
	webSearchStateService interfaces.WebSearchStateService,
	webSearchProviderRepo interfaces.WebSearchProviderRepository,
	graphRepository interfaces.RetrieveGraphRepository,
	chunkRepository interfaces.ChunkRepository,
	knowledgeRepository interfaces.KnowledgeRepository,
	governanceRepository interfaces.KnowledgeGovernanceRepository,
) *PluginSearchParallel {
	// Create internal plugins without registering them
	searchPlugin := &PluginSearch{
		knowledgeBaseService:  knowledgeBaseService,
		knowledgeService:      knowledgeService,
		chunkService:          chunkService,
		config:                config,
		webSearchService:      webSearchService,
		tenantService:         tenantService,
		sessionService:        sessionService,
		webSearchStateService: webSearchStateService,
		webSearchProviderRepo: webSearchProviderRepo,
		governanceRepo:        governanceRepository,
	}

	searchEntityPlugin := &PluginSearchEntity{
		graphRepo:      graphRepository,
		chunkRepo:      chunkRepository,
		knowledgeRepo:  knowledgeRepository,
		governanceRepo: governanceRepository,
	}

	res := &PluginSearchParallel{
		knowledgeBaseService: knowledgeBaseService,
		knowledgeService:     knowledgeService,
		config:               config,
		webSearchService:     webSearchService,
		tenantService:        tenantService,
		sessionService:       sessionService,
		graphRepo:            graphRepository,
		chunkRepo:            chunkRepository,
		knowledgeRepo:        knowledgeRepository,
		searchPlugin:         searchPlugin,
		searchEntityPlugin:   searchEntityPlugin,
	}
	eventManager.Register(res)
	return res
}

// ActivationEvents returns the event types this plugin handles
func (p *PluginSearchParallel) ActivationEvents() []types.EventType {
	return []types.EventType{types.CHUNK_SEARCH_PARALLEL}
}

// OnEvent handles parallel search events - runs chunk search and entity search concurrently
func (p *PluginSearchParallel) OnEvent(ctx context.Context,
	eventType types.EventType, chatManage *types.ChatManage, next func() *PluginError,
) *PluginError {
	// Intent-based skip: query-understand step determined KB retrieval is unnecessary
	if !chatManage.NeedsRetrieval() {
		pipelineInfo(ctx, "SearchParallel", "skip", map[string]interface{}{
			"session_id": chatManage.SessionID,
			"reason":     "intent_no_search",
		})
		return next()
	}
	stageID, stageStarted := emitPipelineStageStart(ctx, chatManage, "knowledge_search", "检索知识库")

	pipelineInfo(ctx, "SearchParallel", "start", map[string]interface{}{
		"session_id":   chatManage.SessionID,
		"has_entities": len(chatManage.Entity) > 0,
		"query_bytes":  len([]byte(chatManage.RewriteQuery)),
	})

	// Deep-copy to avoid concurrent read/write on shared slice fields
	chunkCM := chatManage.Clone()
	chunkCM.SearchResult = nil
	entityCM := chatManage.Clone()
	entityCM.SearchResult = nil

	noop := func() *PluginError { return nil }
	skip := AssessGraphSkip(chatManage)
	graphEnabled := skip.Layer1Allowed
	skipReason := skip.Reason
	if !graphEnabled {
		if skipReason == "" {
			skipReason = GraphReasonRelationNotNeeded
		}
	} else if len(chatManage.EntityKBIDs) == 0 && len(chatManage.EntityKnowledge) == 0 {
		skipReason = GraphReasonNoGraphKB
	} else if len(chatManage.Entity) == 0 {
		skipReason = GraphReasonNoEntities
	} else {
		skipReason = ""
	}

	tasks := []ParallelTask{
		{
			Name: "chunk_search",
			Run: func() *PluginError {
				err := p.searchPlugin.OnEvent(ctx, types.CHUNK_SEARCH, chunkCM, noop)
				pipelineInfo(ctx, "SearchParallel", "chunk_search_done", map[string]interface{}{
					"result_count": len(chunkCM.SearchResult),
					"has_error":    err != nil && err != ErrSearchNothing,
				})
				if err == ErrSearchNothing {
					return nil
				}
				return err
			},
		},
		{
			Name: "entity_search",
			Run: func() *PluginError {
				if !graphEnabled || skipReason != "" {
					pipelineInfo(ctx, "SearchParallel", "entity_search_skip", map[string]interface{}{
						"reason":        skipReason,
						"reason_legacy": skip.ReasonLegacy,
					})
					return nil
				}
				err := p.searchEntityPlugin.OnEvent(ctx, types.ENTITY_SEARCH, entityCM, noop)
				pipelineInfo(ctx, "SearchParallel", "entity_search_done", map[string]interface{}{
					"result_count": len(entityCM.SearchResult),
					"has_error":    err != nil && err != ErrSearchNothing,
				})
				if err == ErrSearchNothing {
					return nil
				}
				return err
			},
		},
	}

	errs := RunParallel(tasks...)

	// Merge results from both searches
	chatManage.SearchResult = interleaveSearchResults(chunkCM.SearchResult, entityCM.SearchResult)
	chatManage.SearchResult = removeDuplicateResults(chatManage.SearchResult)
	chatManage.GraphResult = entityCM.GraphResult
	chatManage.GraphSearchResult = entityCM.GraphSearchResult
	chatManage.GraphContext = entityCM.GraphContext

	for name, err := range errs {
		logger.Warnf(ctx, "[SearchParallel] %s error: %v", name, err.Err)
	}

	pipelineInfo(ctx, "SearchParallel", "complete", map[string]interface{}{
		"session_id":     chatManage.SessionID,
		"chunk_results":  len(chunkCM.SearchResult),
		"entity_results": len(entityCM.SearchResult),
		"total_results":  len(chatManage.SearchResult),
		"error_count":    len(errs),
	})

	if len(chatManage.SearchResult) == 0 {
		if err, ok := errs["chunk_search"]; ok {
			emitPipelineStageResult(ctx, chatManage, stageID, "knowledge_search", "知识检索失败", stageStarted, false, map[string]interface{}{"status": "failed", "result_count": 0})
			return err
		}
		emitPipelineStageResult(ctx, chatManage, stageID, "knowledge_search", "常规检索暂无候选内容", stageStarted, true, map[string]interface{}{"status": "empty", "result_count": 0})
		return ErrSearchNothing
	}

	emitPipelineStageResult(ctx, chatManage, stageID, "knowledge_search", "知识检索完成", stageStarted, true, map[string]interface{}{"status": "completed", "result_count": len(chatManage.SearchResult)})
	return next()
}

// Interleave ranked channels before the rerank budget is applied so graph
// evidence is not systematically excluded by a full ordinary recall list.
func interleaveSearchResults(channels ...[]*types.SearchResult) []*types.SearchResult {
	var results []*types.SearchResult
	for rank := 0; ; rank++ {
		added := false
		for _, channel := range channels {
			if rank < len(channel) {
				results = append(results, channel[rank])
				added = true
			}
		}
		if !added {
			return results
		}
	}
}
