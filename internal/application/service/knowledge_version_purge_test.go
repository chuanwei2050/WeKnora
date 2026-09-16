package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type purgeChunkStub struct {
	interfaces.ChunkService
	chunks         []*types.Chunk
	deletedIDs     []string
	listKnowledgeID string
}

func (s *purgeChunkStub) ListChunksByKnowledgeID(_ context.Context, knowledgeID string) ([]*types.Chunk, error) {
	s.listKnowledgeID = knowledgeID
	return s.chunks, nil
}

func (s *purgeChunkStub) DeleteChunks(_ context.Context, ids []string) error {
	s.deletedIDs = append(s.deletedIDs, ids...)
	return nil
}

type purgeEngineStub struct {
	interfaces.RetrieveEngineService
	deletedChunkIDs []string
	indexedChunkIDs []string
}

func (s *purgeEngineStub) EngineType() types.RetrieverEngineType {
	return types.PostgresRetrieverEngineType
}

func (s *purgeEngineStub) Support() []types.RetrieverType {
	return []types.RetrieverType{types.KeywordsRetrieverType, types.VectorRetrieverType}
}

func (s *purgeEngineStub) DeleteByChunkIDList(_ context.Context, chunkIDs []string, _ int, _ string) error {
	s.deletedChunkIDs = append(s.deletedChunkIDs, chunkIDs...)
	return nil
}

func TestPurgeSupersededVersionRemovesOnlyOldVersionFromIndexes(t *testing.T) {
	chunks := []*types.Chunk{
		{ID: "old-1", KnowledgeID: "doc-1", KnowledgeVersionID: "version-old", ChunkType: types.ChunkTypeText, Content: "old"},
		{ID: "old-2", KnowledgeID: "doc-1", KnowledgeVersionID: "version-old", ChunkType: types.ChunkTypeText, Content: "old2"},
		{ID: "new-1", KnowledgeID: "doc-1", KnowledgeVersionID: "version-new", ChunkType: types.ChunkTypeText, Content: "new"},
		{ID: "parent", KnowledgeID: "doc-1", KnowledgeVersionID: "version-old", ChunkType: types.ChunkTypeParentText, Content: "parent"},
	}
	chunkSvc := &purgeChunkStub{chunks: chunks}
	engine := &purgeEngineStub{}

	err := purgeSupersededVersionIndexes(context.Background(), supersededVersionPurgeDeps{
		chunkService: chunkSvc,
		deleteIndexes: func(ctx context.Context, chunkIDs []string, dimension int, knowledgeType string) error {
			return engine.DeleteByChunkIDList(ctx, chunkIDs, dimension, knowledgeType)
		},
		knowledgeType: types.KnowledgeBaseTypeDocument,
		dimension:     8,
	}, "doc-1", "version-old")
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if chunkSvc.listKnowledgeID != "doc-1" {
		t.Fatalf("listed knowledge %q", chunkSvc.listKnowledgeID)
	}
	if len(engine.deletedChunkIDs) != 2 {
		t.Fatalf("deleted chunk ids = %v, want old-1 and old-2", engine.deletedChunkIDs)
	}
	got := map[string]bool{}
	for _, id := range engine.deletedChunkIDs {
		got[id] = true
	}
	if !got["old-1"] || !got["old-2"] {
		t.Fatalf("deleted chunk ids = %v", engine.deletedChunkIDs)
	}
	if got["new-1"] || got["parent"] {
		t.Fatalf("must not delete current or parent-only index rows: %v", engine.deletedChunkIDs)
	}
	if len(chunkSvc.deletedIDs) != 3 {
		t.Fatalf("DB deletes = %v, want old-1, old-2, parent", chunkSvc.deletedIDs)
	}
	dbGot := map[string]bool{}
	for _, id := range chunkSvc.deletedIDs {
		dbGot[id] = true
	}
	if !dbGot["old-1"] || !dbGot["old-2"] || !dbGot["parent"] || dbGot["new-1"] {
		t.Fatalf("DB deletes = %v", chunkSvc.deletedIDs)
	}
}

func TestStaleChunkIDsForDeletionKeepsCurrentAndPending(t *testing.T) {
	chunks := []*types.Chunk{
		{ID: "cur", KnowledgeVersionID: "v-cur"},
		{ID: "pend", KnowledgeVersionID: "v-pend"},
		{ID: "old", KnowledgeVersionID: "v-old"},
		{ID: "empty", KnowledgeVersionID: ""},
	}
	got := staleChunkIDsForDeletion(chunks, "v-cur", "v-pend")
	if len(got) != 2 {
		t.Fatalf("got %v, want old+empty", got)
	}
	m := map[string]bool{}
	for _, id := range got {
		m[id] = true
	}
	if !m["old"] || !m["empty"] || m["cur"] || m["pend"] {
		t.Fatalf("got %v", got)
	}
}

func TestIndexableChunksForVersionKeepsOnlyTargetVersion(t *testing.T) {
	chunks := []*types.Chunk{
		{ID: "old-1", KnowledgeID: "doc-1", KnowledgeVersionID: "version-old", ChunkType: types.ChunkTypeText, Content: "old", IsEnabled: true},
		{ID: "new-1", KnowledgeID: "doc-1", KnowledgeVersionID: "version-new", ChunkType: types.ChunkTypeText, Content: "new", IsEnabled: true},
		{ID: "parent", KnowledgeID: "doc-1", KnowledgeVersionID: "version-new", ChunkType: types.ChunkTypeParentText, Content: "parent", IsEnabled: true},
	}
	got := indexableChunksForVersion(chunks, "version-new", "Title")
	if len(got) != 1 {
		t.Fatalf("indexable = %d, want 1", len(got))
	}
	if got[0].ChunkID != "new-1" {
		t.Fatalf("chunk id = %q", got[0].ChunkID)
	}
	if !strings.HasPrefix(got[0].Content, "Title\n") {
		t.Fatalf("content = %q", got[0].Content)
	}
}

