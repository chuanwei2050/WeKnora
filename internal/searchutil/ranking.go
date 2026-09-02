package searchutil

import (
	"sort"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

// CompositeSearchScore combines rerank, recall, source, and position signals.
func CompositeSearchScore(result *types.SearchResult, modelScore, baseScore float64, applyPositionPrior bool) float64 {
	sourceWeight := 1.0
	if strings.ToLower(result.KnowledgeSource) == "web_search" {
		sourceWeight = 0.95
	}

	positionPrior := 1.0
	if applyPositionPrior {
		positionRatio := 1.0 - float64(result.StartAt)/float64(result.EndAt+1)
		positionPrior += ClampFloat(positionRatio, -0.05, 0.05)
	}

	composite := (0.6*modelScore + 0.3*baseScore + 0.1*sourceWeight) * positionPrior
	return ClampFloat(composite, 0, 1)
}

// SortSearchResults orders results deterministically without changing scores.
func SortSearchResults(results []*types.SearchResult) {
	sort.SliceStable(results, func(i, j int) bool {
		left, right := results[i], results[j]
		if left == nil || right == nil {
			return right == nil && left != nil
		}
		if left.Score != right.Score {
			return left.Score > right.Score
		}
		if left.KnowledgeBaseID != right.KnowledgeBaseID {
			return left.KnowledgeBaseID < right.KnowledgeBaseID
		}
		if left.KnowledgeID != right.KnowledgeID {
			return left.KnowledgeID < right.KnowledgeID
		}
		if left.ID != right.ID {
			return left.ID < right.ID
		}
		if left.ChunkIndex != right.ChunkIndex {
			return left.ChunkIndex < right.ChunkIndex
		}
		if left.StartAt != right.StartAt {
			return left.StartAt < right.StartAt
		}
		return left.EndAt < right.EndAt
	})
}
