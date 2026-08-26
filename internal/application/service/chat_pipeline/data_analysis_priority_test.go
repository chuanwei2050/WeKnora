package chatpipeline

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestMergeDataAnalysisResultPrioritizesNonEmptySQLResultWithoutDroppingEvidence(t *testing.T) {
	documentResult := &types.SearchResult{ID: "document", Content: "department statistics"}
	analysisResult := &types.SearchResult{ID: "analysis", Content: "exact person"}

	got := mergeDataAnalysisResult(
		[]*types.SearchResult{documentResult},
		analysisResult,
		map[string]interface{}{"row_count": 1},
	)

	if len(got) != 2 || got[0] != analysisResult || got[1] != documentResult {
		t.Fatalf("expected SQL result followed by retrieval evidence, got %#v", got)
	}
}

func TestMergeDataAnalysisResultKeepsRetrievalFallbackForEmptySQLResult(t *testing.T) {
	documentResult := &types.SearchResult{ID: "document", Content: "retrieval fallback"}
	analysisResult := &types.SearchResult{ID: "analysis", Content: "no rows"}

	got := mergeDataAnalysisResult(
		[]*types.SearchResult{documentResult},
		analysisResult,
		map[string]interface{}{"row_count": 0},
	)

	if len(got) != 2 || got[0] != documentResult || got[1] != analysisResult {
		t.Fatalf("expected retrieval fallback followed by SQL result, got %#v", got)
	}
}
