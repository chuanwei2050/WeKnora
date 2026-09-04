package chatpipeline

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestSelectDataAnalysisTargetPrefersExplicitDocument(t *testing.T) {
	ranked := &types.SearchResult{KnowledgeID: "similar", KnowledgeFilename: "similar.xlsx"}
	explicit := &types.SearchResult{KnowledgeID: "requested", KnowledgeFilename: "requested.xlsx"}
	targets := types.SearchTargets{{Type: types.SearchTargetTypeKnowledge, KnowledgeIDs: []string{"requested"}}}

	if got := selectDataAnalysisTarget([]*types.SearchResult{ranked, explicit}, nil, targets); got != explicit {
		t.Fatalf("expected explicitly scoped document, got %#v", got)
	}
}

func TestSelectDataAnalysisTargetUsesRankingForKnowledgeBaseScope(t *testing.T) {
	first := &types.SearchResult{KnowledgeID: "first", KnowledgeFilename: "first.xlsx"}
	second := &types.SearchResult{KnowledgeID: "second", KnowledgeFilename: "second.xlsx"}

	if got := selectDataAnalysisTarget([]*types.SearchResult{first, second}, nil, nil); got != first {
		t.Fatalf("expected highest-ranked table, got %#v", got)
	}
}
