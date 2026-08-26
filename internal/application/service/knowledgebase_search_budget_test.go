package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestResolveRetrievalMatchCountsUsesConfiguredTotals(t *testing.T) {
	vector, keyword := resolveRetrievalMatchCounts(types.SearchParams{
		MatchCount:        5,
		VectorMatchCount:  50,
		KeywordMatchCount: 50,
	})

	if vector != 50 || keyword != 50 {
		t.Fatalf("retrieval counts = %d/%d, want configured totals 50/50", vector, keyword)
	}
}

func TestResolveRetrievalMatchCountsKeepsDefaultsAndCap(t *testing.T) {
	vector, keyword := resolveRetrievalMatchCounts(types.SearchParams{MatchCount: 5})
	if vector != 50 || keyword != 50 {
		t.Fatalf("default retrieval counts = %d/%d, want 50/50", vector, keyword)
	}

	vector, keyword = resolveRetrievalMatchCounts(types.SearchParams{
		VectorMatchCount:  800,
		KeywordMatchCount: 900,
	})
	if vector != 500 || keyword != 500 {
		t.Fatalf("capped retrieval counts = %d/%d, want 500/500", vector, keyword)
	}
}
