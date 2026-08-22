package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestBuildSearchResultPreservesKnowledgeVersionID(t *testing.T) {
	chunk := &types.Chunk{
		ID:                 "chunk-1",
		KnowledgeID:        "knowledge-1",
		KnowledgeVersionID: "version-1",
	}
	knowledge := &types.Knowledge{ID: chunk.KnowledgeID}

	result := (&knowledgeBaseService{}).buildSearchResult(chunk, knowledge, 0.8, types.MatchTypeEmbedding, "")

	if result.KnowledgeVersionID != chunk.KnowledgeVersionID {
		t.Fatalf("knowledge version ID = %q, want %q", result.KnowledgeVersionID, chunk.KnowledgeVersionID)
	}
}

func TestVectorRetrievalUsesPlatformEmbeddingWhenKnowledgeBaseModelIDIsEmpty(t *testing.T) {
	kb := &types.KnowledgeBase{
		IndexingStrategy: types.IndexingStrategy{VectorEnabled: true},
	}
	if !shouldUseVectorRetrieval(kb) {
		t.Fatal("vector-enabled knowledge base must not be disabled by an empty legacy model ID")
	}

	kb.IndexingStrategy.VectorEnabled = false
	if shouldUseVectorRetrieval(kb) {
		t.Fatal("vector-disabled knowledge base must not use vector retrieval")
	}
}
