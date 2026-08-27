package chatpipeline

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const (
	maxDirectLoadChunks = 50
	maxDirectLoadBytes  = 100_000
)

// PluginSearch implements search functionality for chat pipeline
type PluginSearch struct {
	knowledgeBaseService  interfaces.KnowledgeBaseService
	knowledgeService      interfaces.KnowledgeService
	chunkService          interfaces.ChunkService
	config                *config.Config
	webSearchService      interfaces.WebSearchService
	tenantService         interfaces.TenantService
	sessionService        interfaces.SessionService
	webSearchStateService interfaces.WebSearchStateService
	webSearchProviderRepo interfaces.WebSearchProviderRepository
	governanceRepo        interfaces.KnowledgeGovernanceRepository
}

func NewPluginSearch(eventManager *EventManager,
	knowledgeBaseService interfaces.KnowledgeBaseService,
	knowledgeService interfaces.KnowledgeService,
	chunkService interfaces.ChunkService,
	config *config.Config,
	webSearchService interfaces.WebSearchService,
	tenantService interfaces.TenantService,
	sessionService interfaces.SessionService,
	webSearchStateService interfaces.WebSearchStateService,
	webSearchProviderRepo interfaces.WebSearchProviderRepository,
	governanceRepo interfaces.KnowledgeGovernanceRepository,
) *PluginSearch {
	res := &PluginSearch{
		knowledgeBaseService:  knowledgeBaseService,
		knowledgeService:      knowledgeService,
		chunkService:          chunkService,
		config:                config,
		webSearchService:      webSearchService,
		tenantService:         tenantService,
		sessionService:        sessionService,
		webSearchStateService: webSearchStateService,
		webSearchProviderRepo: webSearchProviderRepo,
		governanceRepo:        governanceRepo,
	}
	eventManager.Register(res)
	return res
}

// ActivationEvents returns the event types this plugin handles
func (p *PluginSearch) ActivationEvents() []types.EventType {
	return []types.EventType{types.CHUNK_SEARCH}
}

// OnEvent handles search events in the chat pipeline
func (p *PluginSearch) OnEvent(ctx context.Context,
	eventType types.EventType, chatManage *types.ChatManage, next func() *PluginError,
) *PluginError {
	// Check if we have search targets or web search enabled
	hasKBTargets := len(chatManage.SearchTargets) > 0 || len(chatManage.KnowledgeBaseIDs) > 0 || len(chatManage.KnowledgeIDs) > 0
	if !hasKBTargets && !chatManage.WebSearchEnabled {
		pipelineError(ctx, "Search", "kb_not_found", map[string]interface{}{
			"session_id": chatManage.SessionID,
		})
		return nil
	}

	pipelineInfo(ctx, "Search", "input", map[string]interface{}{
		"session_id":     chatManage.SessionID,
		"rewrite_query":  chatManage.RewriteQuery,
		"search_targets": len(chatManage.SearchTargets),
		"tenant_id":      chatManage.TenantID,
		"web_enabled":    chatManage.WebSearchEnabled,
	})

	// Run KB search and web search concurrently
	pipelineInfo(ctx, "Search", "plan", map[string]interface{}{
		"search_targets":         len(chatManage.SearchTargets),
		"fusion_top_k":           chatManage.EmbeddingTopK,
		"vector_recall_top_k":    chatManage.VectorRecallTopK,
		"keyword_recall_top_k":   chatManage.KeywordRecallTopK,
		"rrf_vector_weight":      chatManage.RRFVectorWeight,
		"rerank_candidate_top_k": chatManage.RerankCandidateTopK,
		"vector_threshold":       chatManage.VectorThreshold,
		"keyword_threshold":      chatManage.KeywordThreshold,
	})
	var wg sync.WaitGroup
	var mu sync.Mutex
	allResults := make([]*types.SearchResult, 0)

	wg.Add(2)
	// Goroutine 1: Knowledge base search using SearchTargets
	go func() {
		defer wg.Done()
		kbResults := p.searchByTargets(ctx, chatManage)
		kbResults = p.filterGovernedSearchResults(ctx, chatManage.TenantID, kbResults)
		if len(kbResults) > 0 {
			mu.Lock()
			allResults = append(allResults, kbResults...)
			mu.Unlock()
		}
	}()

	// Goroutine 2: Web search (if enabled)
	go func() {
		defer wg.Done()
		webResults := p.searchWebIfEnabled(ctx, chatManage)
		if len(webResults) > 0 {
			mu.Lock()
			allResults = append(allResults, webResults...)
			mu.Unlock()
		}
	}()

	wg.Wait()

	chatManage.SearchResult = allResults

	logSearchScoreSample(ctx, "result_score_before_normalize", chatManage.SearchResult)

	// If recall is low, attempt query expansion with keyword-focused search
	if chatManage.EnableQueryExpansion && len(chatManage.SearchResult) < max(1, chatManage.EmbeddingTopK) {
		expResults := p.runQueryExpansion(ctx, chatManage)
		if len(expResults) > 0 {
			chatManage.SearchResult = append(chatManage.SearchResult, expResults...)
		}
	}

	logSearchScoreSample(ctx, "final_score", chatManage.SearchResult)

	// Return if we have results
	if len(chatManage.SearchResult) != 0 {
		pipelineInfo(ctx, "Search", "output", map[string]interface{}{
			"session_id":   chatManage.SessionID,
			"result_count": len(chatManage.SearchResult),
		})
		return next()
	}
	pipelineWarn(ctx, "Search", "output", map[string]interface{}{
		"session_id":   chatManage.SessionID,
		"result_count": 0,
	})
	return ErrSearchNothing
}

func (p *PluginSearch) filterGovernedSearchResults(ctx context.Context, tenantID uint64, results []*types.SearchResult) []*types.SearchResult {
	if len(results) == 0 || p.knowledgeService == nil {
		return results
	}
	ids := make([]string, 0, len(results))
	seen := make(map[string]bool)
	for _, result := range results {
		if result != nil && result.KnowledgeID != "" && !seen[result.KnowledgeID] {
			seen[result.KnowledgeID] = true
			ids = append(ids, result.KnowledgeID)
		}
	}
	knowledges, err := p.knowledgeService.GetKnowledgeBatch(ctx, tenantID, ids)
	if err != nil {
		pipelineWarn(ctx, "Search", "governance_visibility", map[string]interface{}{"error": err.Error()})
		return nil
	}
	byID := make(map[string]*types.Knowledge, len(knowledges))
	for _, knowledge := range knowledges {
		byID[knowledge.ID] = knowledge
	}
	filtered := make([]*types.SearchResult, 0, len(results))
	now := time.Now().UTC()
	for _, result := range results {
		knowledge := byID[result.KnowledgeID]
		if knowledge == nil || knowledge.CurrentVersionID == "" {
			filtered = append(filtered, result)
			continue
		}
		if result.KnowledgeVersionID != knowledge.CurrentVersionID || p.governanceRepo == nil {
			if p.governanceRepo == nil && result.KnowledgeVersionID == knowledge.CurrentVersionID {
				filtered = append(filtered, result)
			}
			continue
		}
		version, err := p.governanceRepo.GetVersion(ctx, tenantID, knowledge.CurrentVersionID)
		if err != nil || version == nil || !version.IsRetrievable(now) {
			continue
		}
		result.KnowledgeLayer = version.SourceMetadata.Layer
		result.SourceCategory = version.SourceMetadata.SourceCategory
		result.EffectiveAt = version.EffectiveAt
		filtered = append(filtered, result)
	}
	return filtered
}

// getSearchResultFromHistory retrieves relevant knowledge references from chat history
func getSearchResultFromHistory(chatManage *types.ChatManage) []*types.SearchResult {
	if len(chatManage.History) == 0 {
		return nil
	}
	// Search history in reverse chronological order
	for i := len(chatManage.History) - 1; i >= 0; i-- {
		if len(chatManage.History[i].KnowledgeReferences) > 0 {
			// Mark all references as history matches
			for _, reference := range chatManage.History[i].KnowledgeReferences {
				reference.MatchType = types.MatchTypeHistory
			}
			return chatManage.History[i].KnowledgeReferences
		}
	}
	return nil
}

func removeDuplicateResults(results []*types.SearchResult) []*types.SearchResult {
	seen := make(map[string]bool)
	contentSig := make(map[string]string) // sig -> first chunk ID
	var uniqueResults []*types.SearchResult
	for _, r := range results {
		// Only deduplicate by exact chunk ID — do NOT treat shared ParentChunkID
		// as duplicates, because different child chunks of the same parent carry
		// different content segments that may all be relevant.
		if seen[r.ID] {
			logger.Debugf(context.Background(), "Dedup: chunk %s removed due to duplicate ID", r.ID)
			continue
		}
		sig := buildContentSignature(r.Content)
		if sig != "" {
			if firstChunk, exists := contentSig[sig]; exists {
				logger.Debugf(context.Background(), "Dedup: chunk %s removed due to content signature (dup of %s, sig prefix: %.50s...)", r.ID, firstChunk, sig)
				continue
			}
			contentSig[sig] = r.ID
		}
		seen[r.ID] = true
		uniqueResults = append(uniqueResults, r)
	}
	return uniqueResults
}

func buildContentSignature(content string) string {
	return searchutil.BuildContentSignature(content)
}

// removePartialOverlaps drops chunks whose content is largely contained within
// a higher-scored chunk, even across different knowledge sources. This catches
// cross-KB duplicates and near-duplicates that exact-signature dedup misses.
//
// Two thresholds are used:
//   - Substring containment: if the normalized short text is a literal substring
//     of the normalized long text, the shorter chunk is removed.
//   - Token overlap coefficient >= 0.85: if 85%+ of the smaller chunk's tokens
//     appear in the larger chunk, the smaller one is redundant.
//
// The input slice MUST already be deduplicated by ID/signature. Within each
// pair the chunk with the lower score is the candidate for removal; ties are
// broken by content length (longer wins).
func removePartialOverlaps(ctx context.Context, results []*types.SearchResult) []*types.SearchResult {
	const overlapThreshold = 0.85

	if len(results) <= 1 {
		return results
	}

	type normEntry struct {
		norm   string
		result *types.SearchResult
	}

	entries := make([]normEntry, 0, len(results))
	for _, r := range results {
		entries = append(entries, normEntry{
			norm:   searchutil.NormalizeContent(r.Content),
			result: r,
		})
	}

	removed := make(map[int]bool)

	for i := 0; i < len(entries); i++ {
		if removed[i] {
			continue
		}
		for j := i + 1; j < len(entries); j++ {
			if removed[j] {
				continue
			}

			a, b := entries[i], entries[j]

			shortIdx, longIdx := i, j
			if len(a.norm) > len(b.norm) {
				shortIdx, longIdx = j, i
			}

			contained := searchutil.IsContentContained(
				entries[shortIdx].norm, entries[longIdx].norm,
			)

			if !contained {
				ratio := searchutil.ContentOverlapRatio(
					entries[shortIdx].result.Content,
					entries[longIdx].result.Content,
				)
				if ratio < overlapThreshold {
					continue
				}
			}

			victim := shortIdx
			if entries[shortIdx].result.Score > entries[longIdx].result.Score {
				victim = longIdx
			}
			removed[victim] = true

			keptIdx := i
			if victim == i {
				keptIdx = j
			}
			pipelineInfo(ctx, "Merge", "partial_overlap_drop", map[string]interface{}{
				"kept_id":    entries[keptIdx].result.ID,
				"dropped_id": entries[victim].result.ID,
				"contained":  contained,
			})
		}
	}

	out := make([]*types.SearchResult, 0, len(results)-len(removed))
	for i, e := range entries {
		if !removed[i] {
			out = append(out, e.result)
		}
	}
	return out
}

func logSearchScoreSample(ctx context.Context, action string, results []*types.SearchResult) {
	const maxLogRows = 8
	limit := min(maxLogRows, len(results))
	for i := 0; i < limit; i++ {
		r := results[i]
		pipelineInfo(ctx, "Search", action, map[string]interface{}{
			"index":      i,
			"chunk_id":   r.ID,
			"score":      fmt.Sprintf("%.4f", r.Score),
			"match_type": r.MatchType,
		})
	}
	if len(results) > limit {
		pipelineInfo(ctx, "Search", action+"_summary", map[string]interface{}{
			"total":     len(results),
			"logged":    limit,
			"truncated": len(results) - limit,
		})
	}
}

// searchByTargets performs KB searches using pre-computed SearchTargets.
// Targets sharing the same underlying embedding model (identified by model
// name + endpoint, not just model ID) are grouped so the query embedding is
// computed once per model AND all full-KB targets in a group are combined into
// a single retrieval call, reducing both embedding API calls and DB round-trips.
func (p *PluginSearch) searchByTargets(
	ctx context.Context,
	chatManage *types.ChatManage,
) []*types.SearchResult {
	if len(chatManage.SearchTargets) == 0 {
		return nil
	}

	queryText := strings.TrimSpace(chatManage.RewriteQuery)

	// Batch-fetch KB records to determine embedding model grouping.
	// On failure, all targets fall into an empty-key group and HybridSearch
	// computes the embedding per-KB (graceful degradation).
	kbIDs := make([]string, 0, len(chatManage.SearchTargets))
	for _, t := range chatManage.SearchTargets {
		kbIDs = append(kbIDs, t.KnowledgeBaseID)
	}
	var kbList []*types.KnowledgeBase
	kbMap := make(map[string]*types.KnowledgeBase)
	if kbs, err := p.knowledgeBaseService.GetKnowledgeBasesByIDsOnly(ctx, kbIDs); err == nil {
		kbList = kbs
		for _, kb := range kbs {
			if kb != nil {
				kbMap[kb.ID] = kb
			}
		}
	} else {
		pipelineWarn(ctx, "Search", "batch_kb_fetch_error", map[string]interface{}{
			"error": err.Error(),
		})
	}

	// Resolve actual model identities (name + endpoint) so that cross-tenant
	// KBs backed by the same physical model share one embedding computation.
	modelKeyMap := p.knowledgeBaseService.ResolveEmbeddingModelKeys(ctx, kbList)

	groups := make(map[string][]*types.SearchTarget)
	for _, t := range chatManage.SearchTargets {
		key := modelKeyMap[t.KnowledgeBaseID] // empty string if unresolved
		groups[key] = append(groups[key], t)
	}

	pipelineInfo(ctx, "Search", "embedding_groups", map[string]interface{}{
		"total_targets": len(chatManage.SearchTargets),
		"unique_models": len(groups),
	})

	var wg sync.WaitGroup
	var mu sync.Mutex
	var results []*types.SearchResult
	directBudget := newDirectLoadBudget()
	directOrder := make(map[*types.SearchTarget]int)
	for _, target := range chatManage.SearchTargets {
		if target != nil && target.Type == types.SearchTargetTypeKnowledge {
			directOrder[target] = len(directOrder)
		}
	}
	groupKeys := make([]string, 0, len(groups))
	for key := range groups {
		groupKeys = append(groupKeys, key)
	}
	sort.Strings(groupKeys)
	taskCount := 0
	for _, key := range groupKeys {
		targets := groups[key]
		hasFullKB := false
		for _, target := range targets {
			if target.Type == types.SearchTargetTypeKnowledgeBase {
				hasFullKB = true
			} else {
				taskCount++
			}
		}
		if hasFullKB {
			taskCount++
		}
	}
	taskIndex := 0

	for _, modelKey := range groupKeys {
		targets := groups[modelKey]
		groupStart := taskIndex
		groupTasks := 0
		hasFullKB := false
		for _, target := range targets {
			if target.Type == types.SearchTargetTypeKnowledgeBase {
				hasFullKB = true
			} else {
				groupTasks++
			}
		}
		if hasFullKB {
			groupTasks++
		}
		taskIndex += groupTasks
		wg.Add(1)
		go func(modelKey string, targets []*types.SearchTarget, groupStart int) {
			defer wg.Done()

			// Compute embedding once for this model group.
			var queryEmbedding []float32
			if modelKey != "" {
				emb, err := p.knowledgeBaseService.GetQueryEmbedding(ctx, targets[0].KnowledgeBaseID, queryText)
				if err != nil {
					pipelineWarn(ctx, "Search", "group_embed_error", map[string]interface{}{
						"model_key": modelKey,
						"kb_id":     targets[0].KnowledgeBaseID,
						"error":     err.Error(),
					})
				} else {
					queryEmbedding = emb
				}
			}

			// Separate full-KB targets (can be combined into one retrieval)
			// from specific-knowledge targets (need per-target direct loading).
			var fullKBIDs []string
			var fullTagIDs []string
			var knowledgeTargets []*types.SearchTarget
			for _, t := range targets {
				if t.Type == types.SearchTargetTypeKnowledgeBase {
					fullKBIDs = append(fullKBIDs, t.KnowledgeBaseID)
					fullTagIDs = append(fullTagIDs, t.TagIDs...)
				} else {
					knowledgeTargets = append(knowledgeTargets, t)
				}
			}

			pipelineInfo(ctx, "Search", "group_plan", map[string]interface{}{
				"model_key":          modelKey,
				"combined_kb_count":  len(fullKBIDs),
				"individual_targets": len(knowledgeTargets),
				"vector_len":         len(queryEmbedding),
			})

			var innerWg sync.WaitGroup
			currentTask := groupStart

			// Combined search: one HybridSearch call spanning all full-KB targets
			if len(fullKBIDs) > 0 {
				budgetIndex := currentTask
				currentTask++
				innerWg.Add(1)
				go func(index int) {
					defer innerWg.Done()
					fusionBudget := searchutil.SplitBudget(chatManage.EmbeddingTopK, taskCount, index)
					vectorBudget := searchutil.SplitBudget(chatManage.VectorRecallTopK, taskCount, index)
					keywordBudget := searchutil.SplitBudget(chatManage.KeywordRecallTopK, taskCount, index)
					if fusionBudget == 0 || (vectorBudget == 0 && keywordBudget == 0) {
						return
					}

					params := types.SearchParams{
						QueryText:             queryText,
						QueryEmbedding:        queryEmbedding,
						KnowledgeBaseIDs:      fullKBIDs,
						TagIDs:                fullTagIDs,
						VectorThreshold:       chatManage.VectorThreshold,
						KeywordThreshold:      chatManage.KeywordThreshold,
						MatchCount:            fusionBudget,
						VectorMatchCount:      vectorBudget,
						RerankCandidateCount:  chatManage.RerankCandidateTopK,
						KeywordMatchCount:     keywordBudget,
						DisableVectorMatch:    vectorBudget == 0,
						DisableKeywordsMatch:  keywordBudget == 0,
						RRFVectorWeight:       chatManage.RRFVectorWeight,
						SkipContextEnrichment: true,
					}
					res, err := p.knowledgeBaseService.HybridSearch(ctx, fullKBIDs[0], params)
					if err != nil {
						pipelineWarn(ctx, "Search", "combined_kb_search_error", map[string]interface{}{
							"kb_ids": fullKBIDs,
							"error":  err.Error(),
						})
						return
					}
					pipelineInfo(ctx, "Search", "combined_kb_result", map[string]interface{}{
						"kb_ids":    fullKBIDs,
						"hit_count": len(res),
					})
					mu.Lock()
					results = append(results, res...)
					mu.Unlock()
				}(budgetIndex)
			}

			// Individual search: per-target handling for specific-knowledge targets
			for _, target := range knowledgeTargets {
				budgetIndex := currentTask
				currentTask++
				innerWg.Add(1)
				go func(t *types.SearchTarget, index int) {
					defer innerWg.Done()
					directBudget.waitTurn(directOrder[t])
					defer directBudget.finishTurn()
					p.searchSingleTarget(ctx, chatManage, t, queryText, queryEmbedding, taskCount, index, directBudget, &mu, &results)
				}(target, budgetIndex)
			}

			innerWg.Wait()
		}(modelKey, targets, groupStart)
	}

	wg.Wait()

	pipelineInfo(ctx, "Search", "kb_result_summary", map[string]interface{}{
		"total_hits": len(results),
	})
	return results
}

func limitRetrievalCandidates(results []*types.SearchResult, configuredLimit int, targets []*types.SearchTarget) []*types.SearchResult {
	direct, regular := preserveBoundedFullDocumentResults(results, targets)
	if len(direct) > 0 {
		regular = limitRetrievalCandidates(regular, configuredLimit, targets)
		return append(direct, regular...)
	}
	results = removeDuplicateResults(results)
	explicitIDs := make([]string, 0)
	seenExplicit := make(map[string]struct{})
	for _, target := range targets {
		if target == nil || target.Type != types.SearchTargetTypeKnowledge {
			continue
		}
		for _, id := range target.KnowledgeIDs {
			if _, exists := seenExplicit[id]; !exists {
				seenExplicit[id] = struct{}{}
				explicitIDs = append(explicitIDs, id)
			}
		}
	}
	limit := configuredLimit
	if limit <= 0 {
		limit = types.DefaultEmbeddingTopK
	}
	if limit <= 0 || len(results) <= limit {
		return results
	}
	selected := make([]*types.SearchResult, 0, limit)
	selectedIDs := make(map[string]struct{}, limit)
	for _, knowledgeID := range explicitIDs {
		if len(selected) == limit {
			break
		}
		var best *types.SearchResult
		for _, result := range results {
			if result != nil && result.KnowledgeID == knowledgeID && (best == nil || result.Score > best.Score) {
				best = result
			}
		}
		if best != nil {
			selected = append(selected, best)
			selectedIDs[best.ID] = struct{}{}
		}
	}
	remaining := make([]*types.SearchResult, 0, len(results)-len(selected))
	for _, result := range results {
		if result == nil {
			continue
		}
		if _, exists := selectedIDs[result.ID]; !exists {
			remaining = append(remaining, result)
		}
	}
	sort.SliceStable(remaining, func(i, j int) bool { return remaining[i].Score > remaining[j].Score })
	remainingLimit := min(limit-len(selected), len(remaining))
	return append(selected, remaining[:remainingLimit]...)
}

func preserveBoundedFullDocumentResults(results []*types.SearchResult, targets []*types.SearchTarget) ([]*types.SearchResult, []*types.SearchResult) {
	byKnowledge := make(map[string][]*types.SearchResult)
	regular := make([]*types.SearchResult, 0, len(results))
	for _, result := range results {
		if result != nil && result.MatchType == types.MatchTypeDirectLoad && result.Metadata["direct_load_reason"] == "full_document_intent" {
			byKnowledge[result.KnowledgeID] = append(byKnowledge[result.KnowledgeID], result)
		} else {
			regular = append(regular, result)
		}
	}
	if len(byKnowledge) == 0 {
		return nil, results
	}
	direct := make([]*types.SearchResult, 0)
	for _, target := range targets {
		if target == nil || target.Type != types.SearchTargetTypeKnowledge {
			continue
		}
		for _, knowledgeID := range target.KnowledgeIDs {
			group := byKnowledge[knowledgeID]
			sort.SliceStable(group, func(i, j int) bool { return group[i].ChunkIndex < group[j].ChunkIndex })
			direct = append(direct, group...)
			delete(byKnowledge, knowledgeID)
		}
	}
	leftoverIDs := make([]string, 0, len(byKnowledge))
	for knowledgeID := range byKnowledge {
		leftoverIDs = append(leftoverIDs, knowledgeID)
	}
	sort.Strings(leftoverIDs)
	for _, knowledgeID := range leftoverIDs {
		group := byKnowledge[knowledgeID]
		sort.SliceStable(group, func(i, j int) bool { return group[i].ChunkIndex < group[j].ChunkIndex })
		direct = append(direct, group...)
	}
	return direct, regular
}

type directLoadBudget struct {
	mu        sync.Mutex
	cond      *sync.Cond
	nextOrder int
	chunks    int
	bytes     int64
	maxChunks int
	maxBytes  int64
}

func newDirectLoadBudget() *directLoadBudget {
	budget := &directLoadBudget{maxChunks: maxDirectLoadChunks, maxBytes: maxDirectLoadBytes}
	budget.cond = sync.NewCond(&budget.mu)
	return budget
}

func (b *directLoadBudget) waitTurn(order int) {
	b.mu.Lock()
	for order != b.nextOrder {
		b.cond.Wait()
	}
}

func (b *directLoadBudget) finishTurn() {
	b.nextOrder++
	b.cond.Broadcast()
	b.mu.Unlock()
}

// searchSingleTarget handles the search logic for a single SearchTarget
// with specific knowledge IDs, including direct chunk loading and HybridSearch.
func (p *PluginSearch) searchSingleTarget(
	ctx context.Context,
	chatManage *types.ChatManage,
	t *types.SearchTarget,
	queryText string,
	queryEmbedding []float32,
	taskCount, taskIndex int,
	directBudget *directLoadBudget,
	mu *sync.Mutex,
	results *[]*types.SearchResult,
) {
	searchKnowledgeIDs := t.KnowledgeIDs

	if t.Type == types.SearchTargetTypeKnowledge && chatManage.Intent == types.IntentSummarize {
		directResults, skippedIDs := p.tryDirectChunkLoading(ctx, chatManage.TenantID, t.KnowledgeBaseID, t.KnowledgeIDs, "full_document_intent", directBudget)

		if len(directResults) > 0 {
			for _, r := range directResults {
				r.KnowledgeBaseID = t.KnowledgeBaseID
			}
			pipelineInfo(ctx, "Search", "direct_load", map[string]interface{}{
				"kb_id":        t.KnowledgeBaseID,
				"loaded_count": len(directResults),
				"skipped_ids":  len(skippedIDs),
			})
			mu.Lock()
			*results = append(*results, directResults...)
			mu.Unlock()
		}

		if len(skippedIDs) == 0 && len(t.KnowledgeIDs) > 0 {
			return
		}
		searchKnowledgeIDs = skippedIDs
	}

	if t.Type == types.SearchTargetTypeKnowledge && len(searchKnowledgeIDs) == 0 {
		return
	}

	fusionBudget := searchutil.SplitBudget(chatManage.EmbeddingTopK, taskCount, taskIndex)
	vectorBudget := searchutil.SplitBudget(chatManage.VectorRecallTopK, taskCount, taskIndex)
	keywordBudget := searchutil.SplitBudget(chatManage.KeywordRecallTopK, taskCount, taskIndex)
	if fusionBudget == 0 || (vectorBudget == 0 && keywordBudget == 0) {
		return
	}
	params := types.SearchParams{
		QueryText:             queryText,
		QueryEmbedding:        queryEmbedding,
		VectorThreshold:       chatManage.VectorThreshold,
		KeywordThreshold:      chatManage.KeywordThreshold,
		MatchCount:            fusionBudget,
		VectorMatchCount:      vectorBudget,
		KeywordMatchCount:     keywordBudget,
		RerankCandidateCount:  chatManage.RerankCandidateTopK,
		RRFVectorWeight:       chatManage.RRFVectorWeight,
		DisableVectorMatch:    vectorBudget == 0,
		DisableKeywordsMatch:  keywordBudget == 0,
		SkipContextEnrichment: true,
	}
	params.TagIDs = t.TagIDs
	if t.Type == types.SearchTargetTypeKnowledge {
		params.KnowledgeIDs = searchKnowledgeIDs
	}
	res, err := p.knowledgeBaseService.HybridSearch(ctx, t.KnowledgeBaseID, params)
	if err != nil {
		pipelineWarn(ctx, "Search", "kb_search_error", map[string]interface{}{
			"kb_id":       t.KnowledgeBaseID,
			"target_type": t.Type,
			"query":       params.QueryText,
			"error":       err.Error(),
		})
		if t.Type != types.SearchTargetTypeKnowledge {
			return
		}
		if !errors.Is(err, searchutil.ErrIndexUnavailable) {
			return
		}
		directResults, _ := p.tryDirectChunkLoading(ctx, chatManage.TenantID, t.KnowledgeBaseID, searchKnowledgeIDs, "index_unavailable", directBudget)
		if len(directResults) == 0 {
			return
		}
		for _, result := range directResults {
			result.KnowledgeBaseID = t.KnowledgeBaseID
		}
		res = directResults
	}
	pipelineInfo(ctx, "Search", "kb_result", map[string]interface{}{
		"kb_id":       t.KnowledgeBaseID,
		"target_type": t.Type,
		"hit_count":   len(res),
	})
	mu.Lock()
	*results = append(*results, res...)
	mu.Unlock()
}

// tryDirectChunkLoading attempts to load chunks for given knowledge IDs directly
// Returns loaded results and a list of knowledge IDs that were skipped (e.g. due to size limits)
func (p *PluginSearch) tryDirectChunkLoading(
	ctx context.Context,
	tenantID uint64,
	knowledgeBaseID string,
	knowledgeIDs []string,
	reason string,
	budget *directLoadBudget,
) ([]*types.SearchResult, []string) {
	if len(knowledgeIDs) == 0 {
		return nil, nil
	}

	var allChunks []*types.Chunk
	var skippedIDs []string
	loadedKnowledgeIDs := make(map[string]bool)
	if budget == nil {
		budget = newDirectLoadBudget()
	}
	knowledges, err := p.knowledgeService.GetKnowledgeBatchWithSharedAccess(ctx, tenantID, knowledgeIDs)
	if err != nil {
		return nil, append(skippedIDs, knowledgeIDs...)
	}
	authorized := make(map[string]*types.Knowledge, len(knowledges))
	for _, knowledge := range knowledges {
		if knowledge != nil && knowledge.KnowledgeBaseID == knowledgeBaseID {
			authorized[knowledge.ID] = knowledge
		}
	}

	for _, kid := range knowledgeIDs {
		knowledge := authorized[kid]
		if knowledge == nil {
			skippedIDs = append(skippedIDs, kid)
			continue
		}
		remainingChunks := budget.maxChunks - budget.chunks
		remainingBytes := budget.maxBytes - budget.bytes
		if remainingChunks <= 0 || remainingBytes <= 0 {
			skippedIDs = append(skippedIDs, kid)
			continue
		}
		chunks, fits, err := p.chunkService.ListChunksByKnowledgeIDBounded(ctx, knowledge.TenantID, kid, remainingChunks, remainingBytes)
		if err != nil {
			logger.Warnf(ctx, "DirectLoad: Failed to load bounded chunks for knowledge %s: %v", kid, err)
			skippedIDs = append(skippedIDs, kid)
			continue
		}
		if !fits {
			logger.Infof(ctx, "DirectLoad: Skipped knowledge %s due to context limit", kid)
			skippedIDs = append(skippedIDs, kid)
			continue
		}
		actualBytes := int64(0)
		for _, chunk := range chunks {
			actualBytes += int64(len([]byte(chunk.Content)))
		}
		budget.chunks += len(chunks)
		budget.bytes += actualBytes
		allChunks = append(allChunks, chunks...)
		loadedKnowledgeIDs[kid] = true
	}

	if len(allChunks) == 0 {
		return nil, skippedIDs
	}

	knowledgeMap := make(map[string]*types.Knowledge)
	for kid := range loadedKnowledgeIDs {
		if knowledge := authorized[kid]; knowledge != nil {
			knowledgeMap[kid] = knowledge
		}
	}

	var results []*types.SearchResult
	for _, chunk := range allChunks {
		res := &types.SearchResult{
			ID:                 chunk.ID,
			Content:            chunk.Content,
			Score:              0,
			KnowledgeID:        chunk.KnowledgeID,
			KnowledgeVersionID: chunk.KnowledgeVersionID,
			ChunkIndex:         chunk.ChunkIndex,
			MatchType:          types.MatchTypeDirectLoad,
			ChunkType:          string(chunk.ChunkType),
			ParentChunkID:      chunk.ParentChunkID,
			StartAt:            chunk.StartAt,
			EndAt:              chunk.EndAt,
		}

		if k, ok := knowledgeMap[chunk.KnowledgeID]; ok {
			res.KnowledgeTitle = k.Title
			res.KnowledgeFilename = k.FileName
			res.KnowledgeSource = k.Source
			res.KnowledgeChannel = k.Channel
		}
		res.Metadata = make(map[string]string, 1)
		res.Metadata["direct_load_reason"] = reason

		results = append(results, res)
	}

	return results, skippedIDs
}

// searchWebIfEnabled executes web search when enabled and returns converted results
func (p *PluginSearch) searchWebIfEnabled(ctx context.Context, chatManage *types.ChatManage) []*types.SearchResult {
	if !chatManage.WebSearchEnabled || p.webSearchService == nil || p.tenantService == nil {
		return nil
	}
	tenant, _ := types.TenantInfoFromContext(ctx)
	providerID := chatManage.WebSearchProviderID

	var webConfig *types.WebSearchConfig
	if tenant != nil && tenant.WebSearchConfig != nil {
		// Clone tenant config so we can safely override MaxResults
		cfg := *tenant.WebSearchConfig
		webConfig = &cfg
	} else if providerID != "" {
		webConfig = &types.WebSearchConfig{
			MaxResults: 10,
		}
	} else {
		pipelineWarn(ctx, "Search", "web_config_missing", map[string]interface{}{
			"tenant_id": chatManage.TenantID,
		})
		return nil
	}

	// Apply agent-level web search overrides
	if chatManage.WebSearchMaxResults > 0 {
		webConfig.MaxResults = chatManage.WebSearchMaxResults
	}

	pipelineInfo(ctx, "Search", "web_request", map[string]interface{}{
		"tenant_id":   chatManage.TenantID,
		"provider_id": providerID,
	})
	webResults, err := p.webSearchService.Search(ctx, providerID, webConfig, chatManage.RewriteQuery)
	if err != nil {
		pipelineWarn(ctx, "Search", "web_search_error", map[string]interface{}{
			"tenant_id": chatManage.TenantID,
			"error":     err.Error(),
		})
		return nil
	}
	// Build questions using RewriteQuery only
	// questions := []string{strings.TrimSpace(chatManage.RewriteQuery)}
	// Load session-scoped temp KB state from Redis using WebSearchStateRepository
	// tempKBID, seen, ids := p.webSearchStateService.GetWebSearchTempKBState(ctx, chatManage.SessionID)
	// compressed, kbID, newSeen, newIDs, err := p.webSearchService.CompressWithRAG(
	// 	ctx, chatManage.SessionID, tempKBID, questions, webResults, webConfig,
	// 	p.knowledgeBaseService, p.knowledgeService, seen, ids,
	// )
	// if err != nil {
	// 	pipelineWarn(ctx, "Search", "web_compress_error", map[string]interface{}{
	// 		"error": err.Error(),
	// 	})
	// } else {
	// 	webResults = compressed
	// 	// Persist temp KB state back into Redis using WebSearchStateRepository
	// 	p.webSearchStateService.SaveWebSearchTempKBState(ctx, chatManage.SessionID, kbID, newSeen, newIDs)
	// }
	res := searchutil.ConvertWebSearchResults(webResults)
	pipelineInfo(ctx, "Search", "web_hits", map[string]interface{}{
		"hit_count": len(res),
	})
	return res
}
