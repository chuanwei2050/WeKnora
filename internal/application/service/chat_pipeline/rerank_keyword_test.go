package chatpipeline

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestPreserveStrongKeywordResultsRestoresOmittedExactMatch(t *testing.T) {
	semantic := &types.SearchResult{ID: "semantic", MatchType: types.MatchTypeEmbedding, Score: 0.8}
	keywordTop := &types.SearchResult{ID: "keyword-top", MatchType: types.MatchTypeKeywords, Score: 21.1}
	exact := &types.SearchResult{ID: "exact", MatchType: types.MatchTypeKeywords, Score: 19.36}
	weak := &types.SearchResult{ID: "weak", MatchType: types.MatchTypeKeywords, Score: 10}

	got := preserveStrongKeywordResults(
		[]*types.SearchResult{semantic, keywordTop},
		[]*types.SearchResult{semantic, keywordTop, exact, weak},
		3,
	)
	if len(got) != 3 {
		t.Fatalf("expected 3 results, got %d", len(got))
	}
	if got[2].ID != exact.ID || got[2].Metadata["keyword_preserved"] != "true" {
		t.Fatalf("strong omitted keyword result was not restored: %#v", got[2])
	}
	if got[2].Score != 1.0 || got[2].Metadata["base_score"] != "19.3600" {
		t.Fatalf("preserved keyword score was not normalized: %#v", got[2])
	}
}

func TestPreserveStrongKeywordResultsDoesNotRestoreWeakMatch(t *testing.T) {
	top := &types.SearchResult{ID: "top", MatchType: types.MatchTypeKeywords, Score: 20}
	weak := &types.SearchResult{ID: "weak", MatchType: types.MatchTypeKeywords, Score: 10}

	got := preserveStrongKeywordResults([]*types.SearchResult{top}, []*types.SearchResult{top, weak}, 3)
	if len(got) != 1 {
		t.Fatalf("weak keyword result should remain filtered, got %d results", len(got))
	}
}
