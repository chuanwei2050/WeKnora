package service

import "github.com/Tencent/WeKnora/internal/types"

// filterRetrievedIndexesByScope is the final trust boundary between an
// external retrieval index and application-visible evidence. Empty filters
// mean that dimension is unrestricted; non-empty filters are combined with
// AND semantics.
func filterRetrievedIndexesByScope(
	results []*types.IndexWithScore,
	knowledgeBaseIDs []string,
	knowledgeIDs []string,
	tagIDs []string,
) []*types.IndexWithScore {
	if len(results) == 0 {
		return results
	}

	allowedKnowledgeBases := stringSet(knowledgeBaseIDs)
	allowedKnowledge := stringSet(knowledgeIDs)
	allowedTags := stringSet(tagIDs)
	filtered := make([]*types.IndexWithScore, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		if !scopeAllows(allowedKnowledgeBases, result.KnowledgeBaseID) ||
			!scopeAllows(allowedKnowledge, result.KnowledgeID) ||
			!scopeAllows(allowedTags, result.TagID) {
			continue
		}
		filtered = append(filtered, result)
	}
	return filtered
}

func filterSearchResultsByScope(
	results []*types.SearchResult,
	knowledgeBaseIDs []string,
	knowledgeIDs []string,
	tagIDs []string,
) []*types.SearchResult {
	if len(results) == 0 {
		return results
	}

	allowedKnowledgeBases := stringSet(knowledgeBaseIDs)
	allowedKnowledge := stringSet(knowledgeIDs)
	allowedTags := stringSet(tagIDs)
	filtered := make([]*types.SearchResult, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		if !scopeAllows(allowedKnowledgeBases, result.KnowledgeBaseID) ||
			!scopeAllows(allowedKnowledge, result.KnowledgeID) ||
			!scopeAllows(allowedTags, result.TagID) {
			continue
		}
		filtered = append(filtered, result)
	}
	return filtered
}

func stringSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func scopeAllows(allowed map[string]struct{}, value string) bool {
	if len(allowed) == 0 {
		return true
	}
	_, ok := allowed[value]
	return ok
}
