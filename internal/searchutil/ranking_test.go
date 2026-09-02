package searchutil

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestCompositeSearchScore(t *testing.T) {
	require.InDelta(t, 0.735, CompositeSearchScore(
		&types.SearchResult{StartAt: 10, EndAt: 100}, 0.8, 0.4, true,
	), 1e-9)
	require.InDelta(t, 0.695, CompositeSearchScore(
		&types.SearchResult{KnowledgeSource: "WEB_SEARCH", StartAt: -1}, 0.8, 0.4, false,
	), 1e-9)
	require.InDelta(t, 0.7, CompositeSearchScore(
		&types.SearchResult{StartAt: 0, EndAt: 0}, 0.5, 1, false,
	), 1e-9)
	require.InDelta(t, 0.735, CompositeSearchScore(
		&types.SearchResult{StartAt: 0, EndAt: 0}, 0.5, 1, true,
	), 1e-9)
}

func TestCompositeSearchScoreMatchesLegacyCallers(t *testing.T) {
	results := []*types.SearchResult{
		{StartAt: -1, EndAt: -1},
		{StartAt: 0, EndAt: 0},
		{StartAt: 0, EndAt: 100},
		{StartAt: 50, EndAt: 100},
		{KnowledgeSource: "web_search", StartAt: 10, EndAt: 100},
	}
	scores := []float64{-0.1, 0, 0.15, 0.5, 0.9, 1.1}
	for _, result := range results {
		for _, modelScore := range scores {
			for _, baseScore := range scores {
				require.Equal(t,
					legacyCompositeSearchScore(result, modelScore, baseScore, result.StartAt >= 0),
					CompositeSearchScore(result, modelScore, baseScore, result.StartAt >= 0),
					"chat caller mismatch for result=%+v model=%v base=%v", result, modelScore, baseScore,
				)
				require.Equal(t,
					legacyCompositeSearchScore(result, modelScore, baseScore, result.StartAt >= 0 && result.EndAt > result.StartAt),
					CompositeSearchScore(result, modelScore, baseScore, result.StartAt >= 0 && result.EndAt > result.StartAt),
					"agent caller mismatch for result=%+v model=%v base=%v", result, modelScore, baseScore,
				)
			}
		}
	}
}

func TestSortSearchResultsUsesStableTieBreakers(t *testing.T) {
	results := []*types.SearchResult{
		{ID: "chunk-b", KnowledgeBaseID: "kb", KnowledgeID: "doc", Score: 0.8},
		nil,
		{ID: "chunk-low", KnowledgeBaseID: "kb", KnowledgeID: "doc", Score: 0.7},
		{ID: "chunk-a", KnowledgeBaseID: "kb", KnowledgeID: "doc", Score: 0.8},
	}

	SortSearchResults(results)

	require.Equal(t, []string{"chunk-a", "chunk-b", "chunk-low"}, []string{
		results[0].ID, results[1].ID, results[2].ID,
	})
	require.Nil(t, results[3])
}

func legacyCompositeSearchScore(result *types.SearchResult, modelScore, baseScore float64, applyPositionPrior bool) float64 {
	sourceWeight := 1.0
	if strings.ToLower(result.KnowledgeSource) == "web_search" {
		sourceWeight = 0.95
	}
	positionPrior := 1.0
	if applyPositionPrior {
		positionPrior += ClampFloat(1.0-float64(result.StartAt)/float64(result.EndAt+1), -0.05, 0.05)
	}
	composite := (0.6*modelScore + 0.3*baseScore + 0.1*sourceWeight) * positionPrior
	if composite < 0 {
		return 0
	}
	if composite > 1 {
		return 1
	}
	return composite
}
