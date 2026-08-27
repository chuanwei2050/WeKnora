package chatpipeline

import (
	"context"
	"regexp"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/retrievalkernel"
	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
)

const (
	maxQueryExpansionVariants  = 3
	maxQueryExpansionCalls     = 8
	maxExpansionGovernanceScan = retrievalkernel.MaxCandidatesPerRequest
)

// runQueryExpansion performs query expansion when initial recall is low.
// It generates query variants and runs concurrent retrieval across search targets.
func (p *PluginSearch) runQueryExpansion(ctx context.Context, chatManage *types.ChatManage, limiter *retrievalkernel.Limiter) []*types.SearchResult {
	pipelineInfo(ctx, "Search", "recall_low", map[string]interface{}{
		"current":   len(chatManage.SearchResult),
		"threshold": chatManage.EmbeddingTopK,
	})
	expansions := p.expandQueries(ctx, chatManage)
	if len(expansions) == 0 {
		return nil
	}

	pipelineInfo(ctx, "Search", "expansion_start", map[string]interface{}{
		"variants": len(expansions),
	})
	expTopK := expansionCandidateLimit(chatManage)
	expKwTh := chatManage.KeywordThreshold * 0.8

	// Concurrent expansion retrieval across queries and search targets
	expResults := make([]*types.SearchResult, 0, expTopK)
	var muExp sync.Mutex
	var wgExp sync.WaitGroup
	jobs := min(len(expansions)*len(chatManage.SearchTargets), maxQueryExpansionCalls)
	pipelineInfo(ctx, "Search", "expansion_concurrency", map[string]interface{}{
		"jobs": jobs,
		"cap":  4,
	})
	taskIndex := 0
	for _, q := range expansions {
		for _, target := range chatManage.SearchTargets {
			if taskIndex == jobs {
				break
			}
			index := taskIndex
			taskIndex++
			wgExp.Add(1)
			go func(q string, t *types.SearchTarget, index int) {
				defer wgExp.Done()
				if !limiter.Acquire(ctx) {
					return
				}
				defer limiter.Release()
				fusionBudget := searchutil.SplitBudget(expTopK, jobs, index)
				vectorBudget := searchutil.SplitBudget(chatManage.VectorRecallTopK, jobs, index)
				keywordBudget := searchutil.SplitBudget(chatManage.KeywordRecallTopK, jobs, index)
				if fusionBudget == 0 || (vectorBudget == 0 && keywordBudget == 0) {
					return
				}
				paramsExp := types.SearchParams{
					QueryText:             q,
					VectorThreshold:       chatManage.VectorThreshold,
					KeywordThreshold:      expKwTh,
					MatchCount:            fusionBudget,
					VectorMatchCount:      vectorBudget,
					KeywordMatchCount:     keywordBudget,
					RerankCandidateCount:  chatManage.RerankCandidateTopK,
					RRFVectorWeight:       chatManage.RRFVectorWeight,
					DisableVectorMatch:    vectorBudget == 0,
					DisableKeywordsMatch:  keywordBudget == 0,
					SkipContextEnrichment: true, // Pipeline handles context assembly in merge stage
				}
				// Apply knowledge ID filter if this is a partial KB search
				if t.Type == types.SearchTargetTypeKnowledge {
					paramsExp.KnowledgeIDs = t.KnowledgeIDs
				}
				res, err := p.knowledgeBaseService.HybridSearch(ctx, t.KnowledgeBaseID, paramsExp)
				if err != nil {
					pipelineWarn(ctx, "Search", "expansion_error", map[string]interface{}{
						"kb_id": t.KnowledgeBaseID,
						"error": err.Error(),
					})
					return
				}
				if len(res) > 0 {
					if len(res) > maxExpansionGovernanceScan {
						res = res[:maxExpansionGovernanceScan]
					}
					for _, r := range res {
						if r != nil {
							r.KnowledgeBaseID = t.KnowledgeBaseID
						}
					}
					res = p.filterGovernedSearchResults(ctx, chatManage.TenantID, types.SearchTargets{t}, res)
					pipelineInfo(ctx, "Search", "expansion_hits", map[string]interface{}{
						"kb_id":       t.KnowledgeBaseID,
						"query_bytes": len([]byte(q)),
						"hits":        len(res),
					})
					muExp.Lock()
					remaining := expTopK - len(expResults)
					if remaining > 0 {
						if len(res) > remaining {
							res = res[:remaining]
						}
						expResults = append(expResults, res...)
					}
					muExp.Unlock()
				}
			}(q, target, index)
		}
		if taskIndex == jobs {
			break
		}
	}
	wgExp.Wait()
	if len(expResults) > 0 {
		pipelineInfo(ctx, "Search", "expansion_done", map[string]interface{}{
			"added": len(expResults),
		})
	}
	return expResults
}

func expansionCandidateLimit(chatManage *types.ChatManage) int {
	return retrievalkernel.BoundCandidateLimit(max(chatManage.EmbeddingTopK, chatManage.RerankTopK*2))
}

// expandQueries generates query variants locally without LLM to improve keyword recall.
// Uses simple techniques: word reordering, stopword removal, key phrase extraction.
func (p *PluginSearch) expandQueries(ctx context.Context, chatManage *types.ChatManage) []string {
	query := strings.TrimSpace(chatManage.RewriteQuery)
	if query == "" {
		return nil
	}

	expansions := make([]string, 0, 3)
	seen := make(map[string]struct{})
	seen[strings.ToLower(query)] = struct{}{}
	if q := strings.ToLower(chatManage.Query); q != "" {
		seen[q] = struct{}{}
	}

	addIfNew := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || len(s) < 3 {
			return
		}
		key := strings.ToLower(s)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		expansions = append(expansions, s)
	}

	// 1. Remove common stopwords and create keyword-only variant
	keywords := extractKeywords(query)
	if len(keywords) >= 2 {
		addIfNew(strings.Join(keywords, " "))
	}

	// 2. Extract quoted phrases or key segments
	phrases := extractPhrases(query)
	for _, phrase := range phrases {
		addIfNew(phrase)
	}

	// 3. Split by common delimiters and use longest segment
	segments := splitByDelimiters(query)
	for _, seg := range segments {
		if len(seg) > 5 {
			addIfNew(seg)
		}
	}

	// 4. Remove question words (什么/如何/怎么/为什么/哪个 etc.)
	cleaned := removeQuestionWords(query)
	if cleaned != query {
		addIfNew(cleaned)
	}

	// Keep expansion fan-out within the request budget.
	if len(expansions) > maxQueryExpansionVariants {
		expansions = expansions[:maxQueryExpansionVariants]
	}

	pipelineInfo(ctx, "Search", "local_expansion_result", map[string]interface{}{
		"variants": len(expansions),
	})
	return expansions
}

// Common Chinese and English stopwords
var stopwords = map[string]struct{}{
	"的": {}, "是": {}, "在": {}, "了": {}, "和": {}, "与": {}, "或": {},
	"a": {}, "an": {}, "the": {}, "is": {}, "are": {}, "was": {}, "were": {},
	"be": {}, "been": {}, "being": {}, "have": {}, "has": {}, "had": {},
	"do": {}, "does": {}, "did": {}, "will": {}, "would": {}, "could": {},
	"should": {}, "may": {}, "might": {}, "must": {}, "can": {},
	"to": {}, "of": {}, "in": {}, "for": {}, "on": {}, "with": {}, "at": {},
	"by": {}, "from": {}, "as": {}, "into": {}, "through": {}, "about": {},
	"what": {}, "how": {}, "why": {}, "when": {}, "where": {}, "which": {},
	"who": {}, "whom": {}, "whose": {},
}

// Question words in Chinese
var questionWords = regexp.MustCompile(`^(什么是|什么|如何|怎么|怎样|为什么|为何|哪个|哪些|谁|何时|何地|请问|请告诉我|帮我|我想知道|我想了解)`)

func extractKeywords(text string) []string {
	words := tokenize(text)
	keywords := make([]string, 0, len(words))
	for _, w := range words {
		lower := strings.ToLower(w)
		if _, isStop := stopwords[lower]; !isStop && len(w) > 1 {
			keywords = append(keywords, w)
		}
	}
	return keywords
}

func extractPhrases(text string) []string {
	// Extract quoted content
	var phrases []string
	re := regexp.MustCompile(`["'"'「」『』]([^"'"'「」『』]+)["'"'「」『』]`)
	matches := re.FindAllStringSubmatch(text, -1)
	for _, m := range matches {
		if len(m) > 1 && len(m[1]) > 2 {
			phrases = append(phrases, m[1])
		}
	}
	return phrases
}

func splitByDelimiters(text string) []string {
	// Split by common delimiters
	re := regexp.MustCompile(`[,，;；、。！？!?\s]+`)
	parts := re.Split(text, -1)
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func removeQuestionWords(text string) string {
	return strings.TrimSpace(questionWords.ReplaceAllString(text, ""))
}

func tokenize(text string) []string {
	return types.Jieba.CutForSearch(strings.TrimSpace(text), true)
}

func shouldExpandQuery(chatManage *types.ChatManage) bool {
	if chatManage == nil || !chatManage.EnableQueryExpansion {
		return false
	}
	if chatManage.RoutingDecision != nil && !chatManage.RoutingDecision.Budget.QueryExpansion {
		return false
	}
	if len(chatManage.SearchResult) == 0 {
		return true
	}
	effectiveCandidates := 0
	for _, result := range chatManage.SearchResult {
		if result == nil {
			continue
		}
		effectiveCandidates++
	}
	if effectiveCandidates == 0 {
		return true
	}
	quality := normalizedRetrievalQuality(chatManage.SearchResult)
	if quality >= 0.65 {
		return false
	}
	return effectiveCandidates < max(1, chatManage.EmbeddingTopK) || quality < 0.35
}

func normalizedRetrievalQuality(results []*types.SearchResult) float64 {
	quality := 0.0
	for _, result := range results {
		if result == nil {
			continue
		}
		score := result.Score
		if result.ScoreDomain == types.RetrievalScoreDomainRRF {
			// Weighted RRF uses k=60 and weights summing to one, so rank one
			// from both channels has a maximum score of 1/(60+1).
			score *= 61
		}
		quality = max(quality, max(0, min(1, score)))
	}
	return quality
}
