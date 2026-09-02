package searchutil

import (
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
