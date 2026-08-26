package chatpipeline

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/models/rerank"
	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

const rerankCacheTTL = 10 * time.Minute

type rerankRedisCache interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
}

// PluginRerank implements reranking functionality for chat pipeline
type PluginRerank struct {
	modelService interfaces.ModelService // Service to access rerank models
	cache        rerankRedisCache
	calls        singleflight.Group
}

// NewPluginRerank creates a new rerank plugin instance
func NewPluginRerank(
	eventManager *EventManager,
	modelService interfaces.ModelService,
	redisClient *redis.Client,
) *PluginRerank {
	res := &PluginRerank{
		modelService: modelService,
		cache:        redisClient,
	}
	eventManager.Register(res)
	return res
}

// ActivationEvents returns the event types this plugin handles
func (p *PluginRerank) ActivationEvents() []types.EventType {
	return []types.EventType{types.CHUNK_RERANK}
}

// OnEvent handles reranking events in the chat pipeline
func (p *PluginRerank) OnEvent(ctx context.Context,
	eventType types.EventType, chatManage *types.ChatManage, next func() *PluginError,
) *PluginError {
	if !chatManage.NeedsRetrieval() {
		return next()
	}
	stageID, stageStarted := emitPipelineStageStart(ctx, chatManage, "rerank", "筛选相关内容")
	beforePrepare := len(chatManage.SearchResult)
	candidateLimit := adaptiveRerankCandidateLimit(chatManage)
	chatManage.SearchResult = prepareRerankCandidates(chatManage.SearchResult, candidateLimit)
	if len(chatManage.SearchResult) != beforePrepare {
		pipelineInfo(ctx, "Rerank", "prepare_candidates", map[string]interface{}{
			"before": beforePrepare,
			"after":  len(chatManage.SearchResult),
			"limit":  candidateLimit,
		})
	}
	pipelineInfo(ctx, "Rerank", "input", map[string]interface{}{
		"session_id":    chatManage.SessionID,
		"candidate_cnt": len(chatManage.SearchResult),
		"rerank_model":  chatManage.RerankModelID,
		"rerank_thresh": chatManage.RerankThreshold,
		"rewrite_query": chatManage.RewriteQuery,
	})
	if len(chatManage.SearchResult) == 0 {
		pipelineInfo(ctx, "Rerank", "skip", map[string]interface{}{
			"reason": "empty_search_result",
		})
		emitPipelineStageResult(ctx, chatManage, stageID, "rerank", "没有需要筛选的内容", stageStarted, true, map[string]interface{}{"status": "skipped", "input_count": 0, "output_count": 0})
		return next()
	}
	// Get rerank model from service
	rerankModel, err := p.modelService.GetRerankModel(ctx, "")
	if err != nil {
		pipelineError(ctx, "Rerank", "get_model", map[string]interface{}{
			"model_id": chatManage.RerankModelID,
			"error":    err.Error(),
		})
		emitPipelineStageResult(ctx, chatManage, stageID, "rerank", "相关内容筛选失败", stageStarted, false)
		return ErrGetRerankModel.WithError(err)
	}

	// Prepare passages for reranking (excluding DirectLoad results)
	var passages []string
	var candidatesToRerank []*types.SearchResult
	var directLoadResults []*types.SearchResult

	for _, result := range chatManage.SearchResult {
		if result.MatchType == types.MatchTypeDirectLoad {
			directLoadResults = append(directLoadResults, result)
			pipelineInfo(ctx, "Rerank", "direct_load_skip", map[string]interface{}{
				"chunk_id": result.ID,
			})
			continue
		}
		// 合并Content和ImageInfo的文本内容
		passage := getEnrichedPassage(ctx, result)
		// Skip passages that become empty after cleaning
		if strings.TrimSpace(passage) == "" {
			pipelineInfo(ctx, "Rerank", "empty_passage_skip", map[string]interface{}{
				"chunk_id": result.ID,
			})
			continue
		}
		passages = append(passages, passage)
		candidatesToRerank = append(candidatesToRerank, result)
	}

	pipelineInfo(ctx, "Rerank", "build_passages", map[string]interface{}{
		"total_cnt":     len(chatManage.SearchResult),
		"candidate_cnt": len(candidatesToRerank),
		"direct_cnt":    len(directLoadResults),
	})

	var rerankResp []rerank.RankResult

	// Only call rerank model if there are candidates
	if len(candidatesToRerank) > 0 {
		// Single rerank call with RewriteQuery, use threshold degradation if no results
		originalThreshold := chatManage.RerankThreshold
		rerankResp = p.rerank(ctx, chatManage, rerankModel, chatManage.RewriteQuery, passages, candidatesToRerank)

		// If no results and threshold is high enough, try with lower threshold
		if len(rerankResp) == 0 && originalThreshold > 0.3 {
			degradedThreshold := originalThreshold * 0.7
			if degradedThreshold < 0.3 {
				degradedThreshold = 0.3
			}
			pipelineWarn(ctx, "Rerank", "threshold_degrade", map[string]interface{}{
				"original":      originalThreshold,
				"degraded":      degradedThreshold,
				"candidate_cnt": len(candidatesToRerank),
				"reason":        "no results above original threshold, retrying with lower threshold",
			})
			chatManage.RerankThreshold = degradedThreshold
			rerankResp = p.rerank(ctx, chatManage, rerankModel, chatManage.RewriteQuery, passages, candidatesToRerank)
			// Restore original threshold
			chatManage.RerankThreshold = originalThreshold
		}
	}

	pipelineInfo(ctx, "Rerank", "model_response", map[string]interface{}{
		"result_cnt": len(rerankResp),
	})

	logRerankInputScoreSample(ctx, chatManage.SearchResult)

	for i := range chatManage.SearchResult {
		chatManage.SearchResult[i].Metadata = ensureMetadata(chatManage.SearchResult[i].Metadata)
	}
	reranked := make([]*types.SearchResult, 0, len(rerankResp)+len(directLoadResults))

	// Process reranked results
	for _, rr := range rerankResp {
		if rr.Index >= len(candidatesToRerank) {
			continue
		}
		sr := candidatesToRerank[rr.Index]
		base := sr.Score
		sr.Metadata["base_score"] = fmt.Sprintf("%.4f", base)
		modelScore := rr.RelevanceScore
		sr.Score = compositeScore(sr, modelScore, base)

		// Apply FAQ score boost if enabled
		if chatManage.FAQPriorityEnabled && chatManage.FAQScoreBoost > 1.0 &&
			sr.ChunkType == string(types.ChunkTypeFAQ) {
			originalScore := sr.Score
			sr.Score = math.Min(sr.Score*chatManage.FAQScoreBoost, 1.0)
			sr.Metadata["faq_boosted"] = "true"
			sr.Metadata["faq_original_score"] = fmt.Sprintf("%.4f", originalScore)
			pipelineInfo(ctx, "Rerank", "faq_boost", map[string]interface{}{
				"chunk_id":       sr.ID,
				"original_score": fmt.Sprintf("%.4f", originalScore),
				"boosted_score":  fmt.Sprintf("%.4f", sr.Score),
				"boost_factor":   chatManage.FAQScoreBoost,
			})
		}

		reranked = append(reranked, sr)
	}

	// Process direct load results (bypass rerank model, assume high relevance)
	for _, sr := range directLoadResults {
		base := sr.Score
		sr.Metadata["base_score"] = fmt.Sprintf("%.4f", base)
		// Assign high model score for direct load items
		modelScore := 1.0
		sr.Score = compositeScore(sr, modelScore, base)
		reranked = append(reranked, sr)
	}
	final := applyMMR(ctx, reranked, chatManage, min(len(reranked), max(1, chatManage.RerankTopK)), 0.7)
	final = preserveStrongKeywordResults(final, chatManage.SearchResult, chatManage.RerankTopK)
	chatManage.RerankResult = final

	// Log composite top scores and MMR selection summary
	topN := min(3, len(reranked))
	for i := 0; i < topN; i++ {
		pipelineInfo(ctx, "Rerank", "composite_top", map[string]interface{}{
			"rank":        i + 1,
			"chunk_id":    reranked[i].ID,
			"base_score":  reranked[i].Metadata["base_score"],
			"final_score": fmt.Sprintf("%.4f", reranked[i].Score),
		})
	}

	if len(chatManage.RerankResult) == 0 {
		pipelineWarn(ctx, "Rerank", "output", map[string]interface{}{
			"filtered_cnt": 0,
		})
		emitPipelineStageResult(ctx, chatManage, stageID, "rerank", "未筛选出相关内容", stageStarted, true, map[string]interface{}{"status": "empty", "input_count": len(chatManage.SearchResult), "output_count": 0})
		return ErrSearchNothing
	}

	pipelineInfo(ctx, "Rerank", "output", map[string]interface{}{
		"filtered_cnt": len(chatManage.RerankResult),
	})
	emitPipelineStageResult(ctx, chatManage, stageID, "rerank", "相关内容筛选完成", stageStarted, true, map[string]interface{}{"status": "completed", "input_count": len(chatManage.SearchResult), "output_count": len(chatManage.RerankResult)})
	return next()
}

// preserveStrongKeywordResults prevents the semantic reranker from completely
// vetoing high-confidence lexical matches. Keyword scores are only compared
// with other keyword scores from the same retrieval request.
func preserveStrongKeywordResults(
	reranked, candidates []*types.SearchResult,
	limit int,
) []*types.SearchResult {
	if limit <= 0 || len(candidates) == 0 {
		return reranked
	}
	maxKeywordScore := 0.0
	for _, candidate := range candidates {
		if candidate.MatchType == types.MatchTypeKeywords && candidate.Score > maxKeywordScore {
			maxKeywordScore = candidate.Score
		}
	}
	if maxKeywordScore <= 0 {
		return reranked
	}

	seen := make(map[string]struct{}, len(reranked))
	for _, result := range reranked {
		seen[result.ID] = struct{}{}
	}
	strongCutoff := maxKeywordScore * 0.9
	for _, candidate := range candidates {
		if candidate.MatchType != types.MatchTypeKeywords || candidate.Score < strongCutoff {
			continue
		}
		if _, exists := seen[candidate.ID]; exists {
			continue
		}
		candidate.Metadata = ensureMetadata(candidate.Metadata)
		candidate.Metadata["base_score"] = fmt.Sprintf("%.4f", candidate.Score)
		candidate.Metadata["keyword_preserved"] = "true"
		candidate.Score = 1.0
		if len(reranked) >= limit {
			reranked = reranked[:limit-1]
		}
		reranked = append(reranked, candidate)
		seen[candidate.ID] = struct{}{}
	}
	return reranked
}

// prepareRerankCandidates removes duplicate chunks/content before model inference
// and keeps the candidate count within the configured retrieval budget.
func prepareRerankCandidates(results []*types.SearchResult, limit int) []*types.SearchResult {
	results = removeDuplicateResults(results)
	if limit > 0 && len(results) > limit {
		return results[:limit]
	}
	return results
}

func adaptiveRerankCandidateLimit(chatManage *types.ChatManage) int {
	configuredMax := chatManage.EmbeddingTopK
	if configuredMax <= 0 {
		configuredMax = 30
	}
	if chatManage.RerankCandidateTopK > 0 {
		return min(configuredMax, chatManage.RerankCandidateTopK)
	}

	target := 25
	if chatManage.RoutingDecision != nil {
		switch chatManage.RoutingDecision.Classification.Level {
		case types.ComplexityL1:
			target = 20
		case types.ComplexityL3, types.ComplexityL4:
			target = 30
		}
	} else {
		query := strings.TrimSpace(chatManage.RewriteQuery)
		if query == "" {
			query = strings.TrimSpace(chatManage.Query)
		}
		switch {
		case isShortEntityQuery(query):
			target = 20
		case isComplexRerankQuery(query):
			target = 30
		}
	}

	return min(configuredMax, target)
}

func isShortEntityQuery(query string) bool {
	if query == "" || len([]rune(query)) > 12 {
		return false
	}
	questionMarkers := []string{"?", "？", "什么", "哪些", "怎么", "如何", "为什么", "是否", "请", "帮我"}
	for _, marker := range questionMarkers {
		if strings.Contains(query, marker) {
			return false
		}
	}
	return true
}

func isComplexRerankQuery(query string) bool {
	if len([]rune(query)) >= 60 {
		return true
	}
	complexMarkers := []string{"对比", "比较", "区别", "分别", "综合分析", "多方面", "因果", "优缺点"}
	for _, marker := range complexMarkers {
		if strings.Contains(query, marker) {
			return true
		}
	}
	return false
}

// rerank performs the actual reranking operation with given query and passages
func (p *PluginRerank) rerank(ctx context.Context,
	chatManage *types.ChatManage, rerankModel rerank.Reranker, query string, passages []string,
	candidates []*types.SearchResult,
) []rerank.RankResult {
	pipelineInfo(ctx, "Rerank", "model_call", map[string]interface{}{
		"query_variant": query,
		"passages":      len(passages),
	})

	// Filter out empty or whitespace-only passages before sending to the API
	var cleanPassages []string
	var cleanCandidates []*types.SearchResult
	for i, p := range passages {
		if strings.TrimSpace(p) != "" {
			cleanPassages = append(cleanPassages, p)
			if i < len(candidates) {
				cleanCandidates = append(cleanCandidates, candidates[i])
			}
		}
	}
	if len(cleanPassages) == 0 {
		pipelineInfo(ctx, "Rerank", "model_call_skip", map[string]interface{}{
			"reason": "all_passages_empty",
		})
		return nil
	}
	passages = cleanPassages
	candidates = cleanCandidates

	rerankResp, err := p.rerankWithCache(ctx, rerankModel, query, passages, candidates)
	if err != nil {
		pipelineError(ctx, "Rerank", "model_call", map[string]interface{}{
			"query_variant": query,
			"error":         err.Error(),
		})
		return nil
	}

	// Log top scores for debugging
	pipelineInfo(ctx, "Rerank", "threshold", map[string]interface{}{
		"threshold": chatManage.RerankThreshold,
	})
	logged := min(5, len(rerankResp))
	for i := range logged {
		if rerankResp[i].Index < len(candidates) {
			pipelineInfo(ctx, "Rerank", "top_score", map[string]interface{}{
				"rank":        i + 1,
				"score":       rerankResp[i].RelevanceScore,
				"chunk_id":    candidates[rerankResp[i].Index].ID,
				"match_type":  candidates[rerankResp[i].Index].MatchType,
				"chunk_type":  candidates[rerankResp[i].Index].ChunkType,
				"content_len": len(candidates[rerankResp[i].Index].Content),
			})
		}
	}
	if len(rerankResp) > logged {
		pipelineInfo(ctx, "Rerank", "top_score_summary", map[string]interface{}{
			"total":     len(rerankResp),
			"logged":    logged,
			"truncated": len(rerankResp) - logged,
		})
	}

	// Filter results based on threshold
	rankFilter := []rerank.RankResult{}
	for _, result := range rerankResp {
		if result.Index >= len(candidates) {
			continue
		}
		if result.RelevanceScore >= chatManage.RerankThreshold {
			rankFilter = append(rankFilter, result)
		}
	}

	// Fallback: if threshold filtering removed all results but the top candidate
	// still has a reasonable score, keep it as a safety net. Skip fallback entirely
	// when the best score is too low — forcing irrelevant results is worse than
	// returning nothing and letting the caller handle the empty-result case.
	const fallbackMinScore = 0.15
	if len(rankFilter) == 0 && len(rerankResp) > 0 && rerankResp[0].RelevanceScore >= fallbackMinScore {
		rankFilter = rerankResp[:1]
		pipelineInfo(ctx, "Rerank", "fallback_top1", map[string]interface{}{
			"reason":    "all_below_threshold",
			"threshold": chatManage.RerankThreshold,
			"top_score": rerankResp[0].RelevanceScore,
		})
	} else if len(rankFilter) == 0 {
		pipelineInfo(ctx, "Rerank", "fallback_skip", map[string]interface{}{
			"reason":    "top_score_too_low",
			"threshold": chatManage.RerankThreshold,
			"top_score": safeTopScore(rerankResp),
		})
	}

	return rankFilter
}

func (p *PluginRerank) rerankWithCache(
	ctx context.Context,
	rerankModel rerank.Reranker,
	query string,
	passages []string,
	candidates []*types.SearchResult,
) ([]rerank.RankResult, error) {
	cacheKey := buildRerankCacheKey(rerankModel.GetModelID(), query, passages, candidates)
	if cached, ok := p.loadRerankCache(ctx, cacheKey); ok {
		pipelineInfo(ctx, "Rerank", "cache_hit", map[string]interface{}{"candidates": len(passages)})
		return cached, nil
	}
	pipelineInfo(ctx, "Rerank", "cache_miss", map[string]interface{}{"candidates": len(passages)})

	value, err, shared := p.calls.Do(cacheKey, func() (interface{}, error) {
		if cached, ok := p.loadRerankCache(ctx, cacheKey); ok {
			return cached, nil
		}
		results, callErr := rerankModel.Rerank(ctx, query, passages)
		if callErr != nil {
			return nil, callErr
		}
		compact := compactRankResults(results)
		p.storeRerankCache(ctx, cacheKey, compact)
		return compact, nil
	})
	if shared {
		pipelineInfo(ctx, "Rerank", "singleflight_shared", map[string]interface{}{"candidates": len(passages)})
	}
	if err != nil {
		return nil, err
	}
	return value.([]rerank.RankResult), nil
}

func buildRerankCacheKey(
	modelID string,
	query string,
	passages []string,
	candidates []*types.SearchResult,
) string {
	type cacheCandidate struct {
		ID      string `json:"id"`
		Passage string `json:"passage"`
	}
	payload := struct {
		Version    int              `json:"version"`
		ModelID    string           `json:"model_id"`
		Query      string           `json:"query"`
		Candidates []cacheCandidate `json:"candidates"`
	}{Version: 1, ModelID: modelID, Query: query, Candidates: make([]cacheCandidate, 0, len(passages))}
	for index, passage := range passages {
		candidateID := ""
		if index < len(candidates) {
			candidateID = candidates[index].ID
		}
		payload.Candidates = append(payload.Candidates, cacheCandidate{ID: candidateID, Passage: passage})
	}
	encoded, _ := json.Marshal(payload)
	hash := sha256.Sum256(encoded)
	return fmt.Sprintf("rerank:result:v1:%x", hash[:])
}

func compactRankResults(results []rerank.RankResult) []rerank.RankResult {
	compact := make([]rerank.RankResult, len(results))
	for index, result := range results {
		compact[index] = rerank.RankResult{Index: result.Index, RelevanceScore: result.RelevanceScore}
	}
	return compact
}

func (p *PluginRerank) loadRerankCache(ctx context.Context, key string) ([]rerank.RankResult, bool) {
	if p.cache == nil {
		return nil, false
	}
	data, err := p.cache.Get(ctx, key).Bytes()
	if err != nil {
		if err != redis.Nil {
			pipelineWarn(ctx, "Rerank", "cache_read", map[string]interface{}{"error": err.Error()})
		}
		return nil, false
	}
	var results []rerank.RankResult
	if err := json.Unmarshal(data, &results); err != nil {
		pipelineWarn(ctx, "Rerank", "cache_decode", map[string]interface{}{"error": err.Error()})
		return nil, false
	}
	return results, true
}

func (p *PluginRerank) storeRerankCache(ctx context.Context, key string, results []rerank.RankResult) {
	if p.cache == nil {
		return
	}
	data, err := json.Marshal(results)
	if err != nil {
		pipelineWarn(ctx, "Rerank", "cache_encode", map[string]interface{}{"error": err.Error()})
		return
	}
	if err := p.cache.Set(ctx, key, data, rerankCacheTTL).Err(); err != nil {
		pipelineWarn(ctx, "Rerank", "cache_write", map[string]interface{}{"error": err.Error()})
		return
	}
	pipelineInfo(ctx, "Rerank", "cache_store", map[string]interface{}{"results": len(results)})
}

// ensureMetadata ensures the metadata is not nil
func ensureMetadata(m map[string]string) map[string]string {
	if m == nil {
		return make(map[string]string)
	}
	return m
}

func safeTopScore(results []rerank.RankResult) float64 {
	if len(results) == 0 {
		return 0
	}
	return results[0].RelevanceScore
}

// compositeScore calculates the composite score for a search result
func compositeScore(sr *types.SearchResult, modelScore, baseScore float64) float64 {
	sourceWeight := 1.0
	switch strings.ToLower(sr.KnowledgeSource) {
	case "web_search":
		sourceWeight = 0.95
	default:
		sourceWeight = 1.0
	}
	positionPrior := 1.0
	if sr.StartAt >= 0 {
		positionPrior += searchutil.ClampFloat(1.0-float64(sr.StartAt)/float64(sr.EndAt+1), -0.05, 0.05)
	}
	composite := 0.6*modelScore + 0.3*baseScore + 0.1*sourceWeight
	composite *= positionPrior
	if composite < 0 {
		composite = 0
	}
	if composite > 1 {
		composite = 1
	}
	return composite
}

// applyMMR applies the MMR algorithm to the search results with pre-computed token sets
func applyMMR(
	ctx context.Context,
	results []*types.SearchResult,
	chatManage *types.ChatManage,
	k int,
	lambda float64,
) []*types.SearchResult {
	if k <= 0 || len(results) == 0 {
		return nil
	}
	pipelineInfo(ctx, "Rerank", "mmr_start", map[string]interface{}{
		"lambda":     lambda,
		"k":          k,
		"candidates": len(results),
	})

	// Pre-compute all token sets concurrently (CPU-bound tokenization)
	allTokenSets := ParallelMap(results, 0, func(i int, r *types.SearchResult) map[string]struct{} {
		return searchutil.TokenizeSimple(getEnrichedPassage(ctx, r))
	})

	selected := make([]*types.SearchResult, 0, k)
	selectedTokenSets := make([]map[string]struct{}, 0, k)
	selectedIndices := make(map[int]struct{})

	for len(selected) < k && len(selectedIndices) < len(results) {
		bestIdx := -1
		bestScore := -1.0

		for i, r := range results {
			if _, isSelected := selectedIndices[i]; isSelected {
				continue
			}

			relevance := r.Score
			redundancy := 0.0

			// Use pre-computed token sets for redundancy calculation
			for _, selTokens := range selectedTokenSets {
				sim := searchutil.Jaccard(allTokenSets[i], selTokens)
				if sim > redundancy {
					redundancy = sim
				}
			}

			mmr := lambda*relevance - (1.0-lambda)*redundancy
			if mmr > bestScore {
				bestScore = mmr
				bestIdx = i
			}
		}

		if bestIdx < 0 {
			break
		}

		selected = append(selected, results[bestIdx])
		selectedTokenSets = append(selectedTokenSets, allTokenSets[bestIdx])
		selectedIndices[bestIdx] = struct{}{}
	}

	// Compute average redundancy among selected using pre-computed token sets
	avgRed := 0.0
	if len(selected) > 1 {
		pairs := 0
		for i := 0; i < len(selectedTokenSets); i++ {
			for j := i + 1; j < len(selectedTokenSets); j++ {
				avgRed += searchutil.Jaccard(selectedTokenSets[i], selectedTokenSets[j])
				pairs++
			}
		}
		if pairs > 0 {
			avgRed /= float64(pairs)
		}
	}
	pipelineInfo(ctx, "Rerank", "mmr_done", map[string]interface{}{
		"selected":       len(selected),
		"avg_redundancy": fmt.Sprintf("%.4f", avgRed),
	})
	return selected
}

// --- Passage cleaning for rerank ---
//
// Rerank models work on semantic text similarity. Markdown formatting, raw URLs,
// image references, table separators, and other structural syntax are noise that
// can dilute the semantic signal. The functions below strip this noise before
// passages are sent to the rerank model.

var (
	// reMarkdownImage matches ![alt](url) — the entire construct is noise.
	// URL group supports one level of balanced parentheses.
	reMarkdownImage = regexp.MustCompile(`!\[[^\]]*\]\([^()\s]*(?:\([^)]*\)[^()\s]*)*\)`)
	// reLinkedImage matches [![alt](img_url)](link_url) — unwrap to ![alt](img_url)
	// so that the subsequent reMarkdownImage pass can remove the image.
	reLinkedImage = regexp.MustCompile(
		`\[!\[([^\]]*)\]\(([^()\s]*(?:\([^)]*\)[^()\s]*)*)\)\]` +
			`\([^()\s]*(?:\([^)]*\)[^()\s]*)*\)`,
	)
	// reMarkdownLink matches [text](url) — we keep the text, drop the URL.
	// URL group supports one level of balanced parentheses.
	reMarkdownLink = regexp.MustCompile(`\[([^\]]+)\]\([^()\s]*(?:\([^)]*\)[^()\s]*)*\)`)
	// reRawURL matches standalone http(s) URLs.
	reRawURL = regexp.MustCompile(`https?://[^\s)\]>]+`)
	// reCodeBlock matches fenced code blocks (```...```).
	reCodeBlock = regexp.MustCompile("(?s)```(?:\\w*)\n?.*?```")
	// reLatexBlock matches block-level LaTeX ($$...$$).
	reLatexBlock = regexp.MustCompile(`(?s)\$\$.*?\$\$`)
	// reTableSep matches table separator rows like |---|---|.
	// Uses [ \t] instead of \s to avoid consuming newlines across rows.
	reTableSep = regexp.MustCompile(`(?m)^[ \t]*\|[ \t:|-]+\|[ \t]*$`)
	// reTableRow matches markdown table data rows like | col1 | col2 |.
	// Uses [ \t] instead of \s to avoid consuming newlines across rows.
	reTableRow = regexp.MustCompile(`(?m)^[ \t]*\|(.+?)\|[ \t]*$`)
	// reHeadingPrefix matches leading # markers in headings.
	reHeadingPrefix = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	// reBlockquote matches leading > markers.
	reBlockquote = regexp.MustCompile(`(?m)^>\s?`)
	// reBoldItalic3 matches ***text*** wrappers (must come before 2 and 1).
	reBoldItalic3 = regexp.MustCompile(`\*{3}(.+?)\*{3}`)
	// reBoldItalic2 matches **text** wrappers.
	reBoldItalic2 = regexp.MustCompile(`\*{2}(.+?)\*{2}`)
	// reBoldItalic1 matches *text* wrappers.
	reBoldItalic1 = regexp.MustCompile(`\*(.+?)\*`)
	// reExcessiveNewlines collapses 3+ consecutive newlines into 2.
	reExcessiveNewlines = regexp.MustCompile(`\n{3,}`)
	// reListMarker matches unordered (- , * ) and ordered (1. ) list prefixes.
	reListMarker = regexp.MustCompile(`(?m)^[\t ]*(?:[-*+]|\d+\.)\s+`)
	// reHTMLTag matches HTML tags like <br>, <div class="...">, etc.
	reHTMLTag = regexp.MustCompile(`</?[a-zA-Z][^>]*>`)
)

// cleanPassageForRerank strips markdown/structural noise from text to produce
// a clean semantic passage for the rerank model. The cleaning is designed to
// preserve all meaningful natural-language content while removing formatting
// that would confuse text-similarity scoring.
func cleanPassageForRerank(text string) string {
	// 1. Remove code blocks (before other patterns to avoid partial matches)
	text = reCodeBlock.ReplaceAllString(text, "")
	// 2. Remove LaTeX block math
	text = reLatexBlock.ReplaceAllString(text, "")
	// 3. Remove HTML tags
	text = reHTMLTag.ReplaceAllString(text, "")
	// 3.5. Unwrap nested [![alt](img_url)](link_url) → ![alt](img_url)
	//      so that the next step removes the full construct cleanly.
	text = reLinkedImage.ReplaceAllString(text, "![$1]($2)")
	// 4. Remove markdown image references entirely
	text = reMarkdownImage.ReplaceAllString(text, "")
	// 5. Convert markdown links to just their display text
	text = reMarkdownLink.ReplaceAllString(text, "$1")
	// 6. Remove standalone raw URLs
	text = reRawURL.ReplaceAllString(text, "")
	// 7. Remove table separator rows
	text = reTableSep.ReplaceAllString(text, "")
	// 7.5. Convert table data rows to plain text (strip | delimiters)
	text = reTableRow.ReplaceAllStringFunc(text, func(match string) string {
		inner := reTableRow.FindStringSubmatch(match)
		if len(inner) < 2 {
			return match
		}
		cells := strings.Split(inner[1], "|")
		var parts []string
		for _, cell := range cells {
			cell = strings.TrimSpace(cell)
			if cell != "" {
				parts = append(parts, cell)
			}
		}
		return strings.Join(parts, ", ")
	})
	// 8. Strip heading markers but keep heading text
	text = reHeadingPrefix.ReplaceAllString(text, "")
	// 9. Strip blockquote markers
	text = reBlockquote.ReplaceAllString(text, "")
	// 10. Unwrap bold/italic markers, keeping inner text (order: *** before ** before *)
	text = reBoldItalic3.ReplaceAllString(text, "$1")
	text = reBoldItalic2.ReplaceAllString(text, "$1")
	text = reBoldItalic1.ReplaceAllString(text, "$1")
	// 11. Strip list markers
	text = reListMarker.ReplaceAllString(text, "")
	// 12. Collapse excessive newlines
	text = reExcessiveNewlines.ReplaceAllString(text, "\n\n")

	return strings.TrimSpace(text)
}

// getEnrichedPassage 合并Content、ImageInfo和GeneratedQuestions的文本内容
func getEnrichedPassage(ctx context.Context, result *types.SearchResult) string {
	combinedText := cleanPassageForRerank(result.Content)
	var enrichments []string

	// 解析ImageInfo
	if result.ImageInfo != "" {
		var imageInfos []types.ImageInfo
		err := json.Unmarshal([]byte(result.ImageInfo), &imageInfos)
		if err != nil {
			pipelineWarn(ctx, "Rerank", "image_info_parse", map[string]interface{}{
				"error": err.Error(),
			})
		} else {
			// 提取所有图片的描述和OCR文本
			for _, img := range imageInfos {
				if img.Caption != "" {
					enrichments = append(enrichments, img.Caption)
				}
				if img.OCRText != "" {
					enrichments = append(enrichments, img.OCRText)
				}
			}
		}
	}

	// 解析ChunkMetadata中的GeneratedQuestions
	if len(result.ChunkMetadata) > 0 {
		var docMeta types.DocumentChunkMetadata
		err := json.Unmarshal(result.ChunkMetadata, &docMeta)
		if err != nil {
			pipelineWarn(ctx, "Rerank", "chunk_metadata_parse", map[string]interface{}{
				"error": err.Error(),
			})
		} else if questionStrings := docMeta.GetQuestionStrings(); len(questionStrings) > 0 {
			enrichments = append(enrichments, strings.Join(questionStrings, "; "))
		}
	}

	if len(enrichments) == 0 {
		return combinedText
	}

	// 组合内容和增强信息
	if combinedText != "" {
		combinedText += "\n\n"
	}
	combinedText += strings.Join(enrichments, "\n")

	return combinedText
}

func logRerankInputScoreSample(ctx context.Context, results []*types.SearchResult) {
	const maxLogRows = 8
	limit := min(maxLogRows, len(results))
	for i := 0; i < limit; i++ {
		sr := results[i]
		pipelineInfo(ctx, "Rerank", "input_score", map[string]interface{}{
			"index":      i,
			"chunk_id":   sr.ID,
			"score":      fmt.Sprintf("%.4f", sr.Score),
			"match_type": sr.MatchType,
		})
	}
	if len(results) > limit {
		pipelineInfo(ctx, "Rerank", "input_score_summary", map[string]interface{}{
			"total":     len(results),
			"logged":    limit,
			"truncated": len(results) - limit,
		})
	}
}
