package chatpipeline

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestFilterTopKPreservesStructuredResult(t *testing.T) {
	doc1 := &types.SearchResult{ID: "doc-1"}
	doc2 := &types.SearchResult{ID: "doc-2"}
	structured := &types.SearchResult{ID: "structured", MatchType: types.MatchTypeDataAnalysis}

	got := filterTopKPreservingStructured([]*types.SearchResult{doc1, doc2, structured}, 1)

	if len(got) != 2 || got[0] != structured || got[1] != doc1 {
		t.Fatalf("structured result was filtered or not prioritized: %#v", got)
	}
}
