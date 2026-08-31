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

func TestBuildSearchResultDoesNotExposePendingManualMetadataOnCurrentChunk(t *testing.T) {
	knowledge := &types.Knowledge{
		ID:               "knowledge-1",
		Type:             types.KnowledgeTypeManual,
		Title:            "pending title",
		FileName:         "pending.md",
		Source:           "pending source",
		Description:      "pending description",
		CurrentVersionID: "version-1",
		PendingVersionID: "version-2",
	}
	if err := knowledge.SetManualMetadata(types.NewManualKnowledgeMetadata("pending secret", types.ManualKnowledgeStatusDraft, 2)); err != nil {
		t.Fatal(err)
	}
	result := (&knowledgeBaseService{}).buildSearchResult(&types.Chunk{
		ID: "chunk-1", KnowledgeID: knowledge.ID, KnowledgeVersionID: "version-1",
	}, knowledge, 0.8, types.MatchTypeEmbedding, "")

	if _, ok := result.Metadata["content"]; ok {
		t.Fatal("pending manual content was exposed in current-version search metadata")
	}
	if result.KnowledgeTitle != "" || result.KnowledgeFilename != "" || result.KnowledgeSource != "" || result.KnowledgeDescription != "" {
		t.Fatalf("pending manual display metadata was exposed: %+v", result)
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
