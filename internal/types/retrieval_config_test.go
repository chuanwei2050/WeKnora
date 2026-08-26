package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeRetrievalConfigFillsLegacyFields(t *testing.T) {
	legacy := &RetrievalConfig{EmbeddingTopK: 10, RerankTopK: 5}
	got := NormalizeRetrievalConfig(legacy)

	require.Equal(t, 10, got.EmbeddingTopK)
	require.Equal(t, DefaultVectorRecallTopK, got.VectorRecallTopK)
	require.Equal(t, DefaultKeywordRecallTopK, got.KeywordRecallTopK)
	require.Equal(t, 10, got.RerankCandidateTopK)
	require.Equal(t, 5, got.RerankTopK)
	require.NoError(t, ValidateRetrievalConfig(got))
}

func TestApplyRetrievalConfigUpdatePreservesExplicitZeroThresholds(t *testing.T) {
	zero := 0.0
	disabled := false
	got := ApplyRetrievalConfigUpdate(nil, RetrievalConfigUpdate{
		RRFVectorWeight:      &zero,
		VectorThreshold:      &zero,
		KeywordThreshold:     &zero,
		RerankThreshold:      &zero,
		EnableQueryExpansion: &disabled,
	})

	require.Zero(t, got.RRFVectorWeight)
	require.Zero(t, got.GetEffectiveRRFVectorWeight())
	require.Zero(t, got.GetEffectiveVectorThreshold())
	require.Zero(t, got.GetEffectiveKeywordThreshold())
	require.Zero(t, got.GetEffectiveRerankThreshold())
	require.False(t, NormalizeRetrievalConfig(&got).EnableQueryExpansion)
	require.NoError(t, ValidateRetrievalConfig(got))
}

func TestValidateRetrievalConfigRejectsInvalidStageOrder(t *testing.T) {
	config := DefaultRetrievalConfig()
	config.RerankCandidateTopK = config.EmbeddingTopK + 1
	require.EqualError(t, ValidateRetrievalConfig(config), "rerank_candidate_top_k must be between 1 and embedding_top_k")

	config = DefaultRetrievalConfig()
	config.RerankTopK = config.RerankCandidateTopK + 1
	require.EqualError(t, ValidateRetrievalConfig(config), "rerank_top_k must be between 1 and rerank_candidate_top_k")
}

func TestNormalizeRetrievalConfigPreservesExplicitPreviousValues(t *testing.T) {
	previous := &RetrievalConfig{
		EmbeddingTopK:        30,
		VectorRecallTopK:     50,
		KeywordRecallTopK:    50,
		RRFVectorWeight:      0.7,
		VectorThreshold:      0.15,
		KeywordThreshold:     0.3,
		RerankCandidateTopK:  20,
		RerankTopK:           5,
		RerankThreshold:      0.2,
		EnableQueryExpansion: false,
		vectorThresholdSet:   true,
		rerankThresholdSet:   true,
		queryExpansionSet:    true,
	}

	got := NormalizeRetrievalConfig(previous)
	require.False(t, got.EnableQueryExpansion)
	require.Equal(t, 0.15, got.VectorThreshold)
	require.Equal(t, 5, got.RerankTopK)
	require.Equal(t, 0.2, got.RerankThreshold)
}

func TestRetrievalConfigScanDefaultsMissingRerankThreshold(t *testing.T) {
	var config RetrievalConfig
	require.NoError(t, config.Scan([]byte(`{"embedding_top_k":30,"rerank_candidate_top_k":20,"rerank_top_k":10}`)))
	require.Equal(t, DefaultRerankThreshold, config.GetEffectiveRerankThreshold())

	require.NoError(t, config.Scan([]byte(`{"embedding_top_k":30,"rerank_candidate_top_k":20,"rerank_top_k":10,"rerank_threshold":0}`)))
	require.Zero(t, config.GetEffectiveRerankThreshold())
}
