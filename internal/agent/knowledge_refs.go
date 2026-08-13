package agent

import "github.com/Tencent/WeKnora/internal/types"

const knowledgeSearchResultsDataKey = "search_results"

// appendKnowledgeRefs collects typed retrieval results from tool data for the
// verified-answer pipeline. Tool output is formatted for the LLM and must not
// be reparsed to recover evidence IDs or content.
func appendKnowledgeRefs(existing []*types.SearchResult, step types.AgentStep) []*types.SearchResult {
	refs := append([]*types.SearchResult(nil), existing...)
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if ref != nil && ref.ID != "" {
			seen[ref.ID] = struct{}{}
		}
	}

	for _, toolCall := range step.ToolCalls {
		if toolCall.Name == "" || toolCall.Result == nil || !toolCall.Result.Success {
			continue
		}
		values, ok := toolCall.Result.Data[knowledgeSearchResultsDataKey].([]*types.SearchResult)
		if !ok {
			continue
		}
		for _, ref := range values {
			if ref == nil || ref.ID == "" {
				continue
			}
			if _, ok := seen[ref.ID]; ok {
				continue
			}
			seen[ref.ID] = struct{}{}
			refs = append(refs, ref)
		}
	}
	return refs
}
