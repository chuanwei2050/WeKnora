package chatpipeline

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// PluginFilterTopK is a plugin that filters search results to keep only the top K items
type PluginFilterTopK struct{}

// NewPluginFilterTopK creates a new instance of PluginFilterTopK and registers it with the event manager
func NewPluginFilterTopK(eventManager *EventManager) *PluginFilterTopK {
	res := &PluginFilterTopK{}
	eventManager.Register(res)
	return res
}

// ActivationEvents returns the event types that this plugin responds to
func (p *PluginFilterTopK) ActivationEvents() []types.EventType {
	return []types.EventType{types.FILTER_TOP_K}
}

// OnEvent handles the FILTER_TOP_K event by filtering results to keep only the top K items
// It can filter MergeResult, RerankResult, or SearchResult depending on which is available
func (p *PluginFilterTopK) OnEvent(ctx context.Context,
	eventType types.EventType, chatManage *types.ChatManage, next func() *PluginError,
) *PluginError {
	if !chatManage.NeedsRetrieval() {
		return next()
	}
	pipelineInfo(ctx, "FilterTopK", "input", map[string]interface{}{
		"session_id": chatManage.SessionID,
		"top_k":      chatManage.RerankTopK,
		"merge_cnt":  len(chatManage.MergeResult),
		"rerank_cnt": len(chatManage.RerankResult),
		"search_cnt": len(chatManage.SearchResult),
	})

	filterTopK := func(searchResult []*types.SearchResult, topK int) []*types.SearchResult {
		if topK > 0 && len(searchResult) > topK {
			pipelineInfo(ctx, "FilterTopK", "filter", map[string]interface{}{
				"before": len(searchResult),
				"after":  topK,
			})
			searchResult = searchResult[:topK]
		}
		return searchResult
	}

	if len(chatManage.MergeResult) > 0 {
		chatManage.MergeResult = filterTopKPreservingStructured(chatManage.MergeResult, chatManage.RerankTopK)
	} else if len(chatManage.RerankResult) > 0 {
		chatManage.RerankResult = filterTopK(chatManage.RerankResult, chatManage.RerankTopK)
	} else if len(chatManage.SearchResult) > 0 && chatManage.RerankOutcome != types.RerankOutcomeNoRelevantResult {
		chatManage.SearchResult = filterTopK(chatManage.SearchResult, chatManage.RerankTopK)
	} else {
		pipelineWarn(ctx, "FilterTopK", "skip", map[string]interface{}{
			"reason": "no_results",
		})
	}

	pipelineInfo(ctx, "FilterTopK", "output", map[string]interface{}{
		"merge_cnt":  len(chatManage.MergeResult),
		"rerank_cnt": len(chatManage.RerankResult),
		"search_cnt": len(chatManage.SearchResult),
	})
	return next()
}

func filterTopKPreservingStructured(results []*types.SearchResult, topK int) []*types.SearchResult {
	structured := make([]*types.SearchResult, 0, 1)
	regular := make([]*types.SearchResult, 0, len(results))
	for _, result := range results {
		if result != nil && result.MatchType == types.MatchTypeDataAnalysis {
			structured = append(structured, result)
			continue
		}
		regular = append(regular, result)
	}
	if topK > 0 && len(regular) > topK {
		regular = regular[:topK]
	}
	return append(structured, regular...)
}
