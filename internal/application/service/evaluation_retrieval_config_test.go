package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestEvaluationRetrievalConfigUsesPlatformSnapshotFromContext(t *testing.T) {
	var want types.RetrievalConfig
	require.NoError(t, want.Scan([]byte(`{
		"embedding_top_k":42,
		"vector_recall_top_k":41,
		"keyword_recall_top_k":40,
		"rrf_vector_weight":0.7,
		"vector_threshold":0.15,
		"keyword_threshold":0.3,
		"rerank_candidate_top_k":12,
		"rerank_top_k":6,
		"rerank_threshold":0.2,
		"enable_query_expansion":false
	}`)))

	ctx := context.WithValue(context.Background(), types.TenantInfoContextKey, &types.Tenant{
		RetrievalConfig: &want,
	})

	got := evaluationRetrievalConfig(ctx)
	require.Equal(t, want.EmbeddingTopK, got.EmbeddingTopK)
	require.Equal(t, want.VectorRecallTopK, got.VectorRecallTopK)
	require.Equal(t, want.KeywordRecallTopK, got.KeywordRecallTopK)
	require.Equal(t, want.RerankCandidateTopK, got.RerankCandidateTopK)
	require.Equal(t, want.RerankTopK, got.RerankTopK)
	require.False(t, got.EnableQueryExpansion)
}

func TestEvaluationRetrievalConfigFallsBackToPlatformDefaults(t *testing.T) {
	require.Equal(t, types.DefaultRetrievalConfig(), evaluationRetrievalConfig(context.Background()))
}
