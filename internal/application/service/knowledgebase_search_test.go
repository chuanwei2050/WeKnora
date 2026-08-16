package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestShouldPrecomputeQueryEmbedding(t *testing.T) {
	tests := []struct {
		name string
		kb   *types.KnowledgeBase
		want bool
	}{
		{
			name: "keyword-only knowledge base ignores stale embedding model",
			kb: &types.KnowledgeBase{
				EmbeddingModelID: "legacy-remote-model",
				IndexingStrategy: types.IndexingStrategy{
					KeywordEnabled: true,
				},
			},
			want: false,
		},
		{
			name: "vector knowledge base without a model cannot precompute",
			kb: &types.KnowledgeBase{
				IndexingStrategy: types.IndexingStrategy{
					VectorEnabled: true,
				},
			},
			want: false,
		},
		{
			name: "vector knowledge base with a model precomputes",
			kb: &types.KnowledgeBase{
				EmbeddingModelID: "local-embedding-model",
				IndexingStrategy: types.IndexingStrategy{
					VectorEnabled: true,
				},
			},
			want: true,
		},
		{
			name: "missing knowledge base cannot precompute",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldPrecomputeQueryEmbedding(tt.kb); got != tt.want {
				t.Fatalf("shouldPrecomputeQueryEmbedding() = %v, want %v", got, tt.want)
			}
		})
	}
}
