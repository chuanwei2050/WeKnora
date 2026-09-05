package chatpipeline

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
)

// filterHistoryResults retrieves history references and filters them by
// textual similarity to the current query. Only references that are above
// a Jaccard similarity threshold are kept, and their scores are discounted
// to reflect that they were not directly retrieved for the current query.
// Results already present in currentResults (by chunk ID) are excluded.
func filterHistoryResults(
	ctx context.Context,
	chatManage *types.ChatManage,
	currentResults []*types.SearchResult,
) []*types.SearchResult {
	const (
		// minSimilarity is the minimum Jaccard similarity between the current
		// query and a history chunk's content for it to be injected.
		minSimilarity = 0.15
		// historyScoreDiscount reduces the original score of history results
		// to rank them below freshly-retrieved results of similar relevance.
		historyScoreDiscount = 0.6
		// maxHistoryResults caps the number of history results injected to
		// avoid overwhelming the context with stale references.
		maxHistoryResults = 3
	)

	raw := getSearchResultFromHistory(chatManage)
	if len(raw) == 0 {
		return nil
	}

	// Build a set of chunk IDs already in current results for fast dedup
	existingIDs := make(map[string]struct{}, len(currentResults))
	for _, r := range currentResults {
		existingIDs[r.ID] = struct{}{}
	}

	// Use RewriteQuery if available (it's the cleaned-up retrieval query),
	// otherwise fall back to the original query.
	query := chatManage.RewriteQuery
	if query == "" {
		query = chatManage.Query
	}
	queryTokens := searchutil.TokenizeSimple(query)

	var filtered []*types.SearchResult
	for _, r := range raw {
		if r == nil {
			continue
		}
		if _, exists := existingIDs[r.ID]; exists {
			continue
		}
		contentTokens := searchutil.TokenizeSimple(r.Content)
		sim := searchutil.Jaccard(queryTokens, contentTokens)
		if sim < minSimilarity {
			pipelineInfo(ctx, "Merge", "history_filter_drop", map[string]interface{}{
				"chunk_id":   r.ID,
				"similarity": sim,
			})
			continue
		}
		copyResult := *r
		copyResult.Metadata = maps.Clone(r.Metadata)
		copyResult.SubChunkID = append([]string(nil), r.SubChunkID...)
		r = &copyResult
		r.MatchType = types.MatchTypeHistory
		r.Score = r.Score * historyScoreDiscount
		r.Metadata = ensureMetadata(r.Metadata)
		r.Metadata["history_similarity"] = strings.TrimRight(strings.TrimRight(
			fmt.Sprintf("%.4f", sim), "0"), ".")
		filtered = append(filtered, r)

		pipelineInfo(ctx, "Merge", "history_filter_keep", map[string]interface{}{
			"chunk_id":   r.ID,
			"similarity": sim,
			"new_score":  r.Score,
		})

		if len(filtered) >= maxHistoryResults {
			break
		}
	}
	return filtered
}

// searchHistory revalidates historical evidence before it enters reranking.
// Read current chunks instead of trusting previously merged or edited content.
func (p *PluginSearch) searchHistory(ctx context.Context, manage *types.ChatManage, current []*types.SearchResult) []*types.SearchResult {
	candidates := filterHistoryResults(ctx, manage, current)
	candidates = p.filterGovernedSearchResults(ctx, manage.TenantID, manage.SearchTargets, candidates)
	if len(candidates) == 0 || p.chunkService == nil {
		return nil
	}
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
	}
	// Knowledge access was checked above using the requesting tenant, including
	// shared KB access. Match every returned chunk back to that authorized document.
	chunks, err := p.chunkService.GetRepository().ListChunksByIDOnly(ctx, ids)
	if err != nil {
		return nil
	}
	byID := make(map[string]*types.Chunk, len(chunks))
	for _, chunk := range chunks {
		if chunk != nil {
			byID[chunk.ID] = chunk
		}
	}
	results := make([]*types.SearchResult, 0, len(candidates))
	for _, candidate := range candidates {
		chunk := byID[candidate.ID]
		if chunk == nil || !chunk.IsEnabled || chunk.DeletedAt.Valid || chunk.KnowledgeID != candidate.KnowledgeID || chunk.KnowledgeBaseID != candidate.KnowledgeBaseID || chunk.KnowledgeVersionID != candidate.KnowledgeVersionID {
			continue
		}
		inScope := manage.SearchTargets == nil
		for _, target := range manage.SearchTargets {
			if target == nil || target.KnowledgeBaseID != chunk.KnowledgeBaseID {
				continue
			}
			if target.Type == types.SearchTargetTypeKnowledge && target.KnowledgeIDs != nil && !slices.Contains(target.KnowledgeIDs, chunk.KnowledgeID) {
				continue
			}
			if target.TagIDs != nil && !slices.Contains(target.TagIDs, chunk.TagID) {
				continue
			}
			inScope = true
			break
		}
		if !inScope {
			continue
		}
		candidate.Content = chunk.Content
		candidate.ChunkType = chunk.ChunkType
		candidate.ChunkIndex = chunk.ChunkIndex
		candidate.StartAt, candidate.EndAt = chunk.StartAt, chunk.EndAt
		candidate.ParentChunkID = chunk.ParentChunkID
		candidate.SubChunkID = nil
		candidate.ImageInfo = chunk.ImageInfo
		candidate.ChunkMetadata = chunk.Metadata
		candidate.MatchedContent = ""
		candidate.KeywordLeader = false
		results = append(results, candidate)
	}
	return results
}
