package chatpipeline

import (
	"context"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const wikiSourcePrior = 0.05

// PluginWikiBoost marks wiki page chunks with a bounded source prior.
// Wiki pages contain pre-synthesized knowledge that is more coherent and
// cross-referenced than raw document chunks, so they should rank higher.
//
// It runs before the reranker in the CHUNK_RERANK chain so the prior participates
// in diversity selection without overwriting raw relevance.
type PluginWikiBoost struct {
	kbService interfaces.KnowledgeBaseService
}

// NewPluginWikiBoost creates and registers the wiki boost plugin
func NewPluginWikiBoost(eventManager *EventManager, kbService interfaces.KnowledgeBaseService) *PluginWikiBoost {
	p := &PluginWikiBoost{
		kbService: kbService,
	}
	eventManager.Register(p)
	return p
}

// ActivationEvents returns the event types this plugin handles
func (p *PluginWikiBoost) ActivationEvents() []types.EventType {
	return []types.EventType{types.CHUNK_RERANK}
}

// OnEvent boosts wiki page chunk scores after reranking
func (p *PluginWikiBoost) OnEvent(
	ctx context.Context,
	eventType types.EventType,
	chatManage *types.ChatManage,
	next func() *PluginError,
) *PluginError {
	// Fast path: skip all work if there are no wiki_page chunks in the result set.
	// This avoids hitting the KB service on every chat turn for non-wiki queries.
	hasWikiChunk := false
	for i := range chatManage.SearchResult {
		if chatManage.SearchResult[i] != nil && chatManage.SearchResult[i].ChunkType == types.ChunkTypeWikiPage {
			hasWikiChunk = true
			break
		}
	}
	if !hasWikiChunk {
		return next()
	}

	// Confirm at least one search target is actually a wiki KB.
	wikiKBs := make(map[string]struct{})
	for _, target := range chatManage.SearchTargets {
		if target == nil {
			continue
		}
		kb, err := p.kbService.GetKnowledgeBaseByIDOnly(ctx, target.KnowledgeBaseID)
		if err == nil && kb != nil && kb.IsWikiEnabled() {
			wikiKBs[target.KnowledgeBaseID] = struct{}{}
		}
	}
	if len(wikiKBs) == 0 {
		return next()
	}

	markedCount := 0
	for i := range chatManage.SearchResult {
		result := chatManage.SearchResult[i]
		if result != nil && result.ChunkType == types.ChunkTypeWikiPage {
			if _, ok := wikiKBs[result.KnowledgeBaseID]; !ok {
				continue
			}
			result.RankingSourcePrior = wikiSourcePrior
			result.RankingSourcePriorKind = "wiki"
			markedCount++
		}
	}

	if markedCount > 0 {
		logger.Infof(ctx, "WikiBoost: marked %d wiki page chunks with bounded prior %.2f", markedCount, wikiSourcePrior)
	}
	return next()
}
