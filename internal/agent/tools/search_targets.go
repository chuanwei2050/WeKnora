package tools

import "github.com/Tencent/WeKnora/internal/types"

// resolveSearchTargets applies an optional LLM-provided KB filter to the
// pre-authorized runtime scope. A bad filter may safely fall back only when
// there is exactly one authorized target; multi-KB scopes are never expanded.
func resolveSearchTargets(
	available types.SearchTargets,
	requestedKBIDs []string,
) (resolved types.SearchTargets, usedSingleTargetFallback bool) {
	if len(requestedKBIDs) == 0 {
		return available, false
	}

	requested := make(map[string]struct{}, len(requestedKBIDs))
	for _, kbID := range requestedKBIDs {
		requested[kbID] = struct{}{}
	}
	for _, target := range available {
		if _, ok := requested[target.KnowledgeBaseID]; ok {
			resolved = append(resolved, target)
		}
	}
	if len(resolved) > 0 {
		return resolved, false
	}
	if len(available) == 1 {
		return available, true
	}
	return nil, false
}
