package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type knowledgeTagBatchKnowledgeRepo struct {
	interfaces.KnowledgeRepository
	knowledge *types.Knowledge
	updated   []*types.Knowledge
}

func (r *knowledgeTagBatchKnowledgeRepo) GetKnowledgeBatch(context.Context, uint64, []string) ([]*types.Knowledge, error) {
	return []*types.Knowledge{r.knowledge}, nil
}

func (r *knowledgeTagBatchKnowledgeRepo) UpdateKnowledgeBatch(_ context.Context, knowledge []*types.Knowledge) error {
	r.updated = knowledge
	return nil
}

type knowledgeTagBatchTagRepo struct {
	interfaces.KnowledgeTagRepository
	tag *types.KnowledgeTag
}

func (r knowledgeTagBatchTagRepo) GetByID(context.Context, uint64, string) (*types.KnowledgeTag, error) {
	return r.tag, nil
}

type knowledgeTagBatchChunkRepo struct {
	interfaces.ChunkRepository
	chunks  []*types.Chunk
	updated []*types.Chunk
}

func (r *knowledgeTagBatchChunkRepo) ListChunksByKnowledgeID(context.Context, uint64, string) ([]*types.Chunk, error) {
	return r.chunks, nil
}

func (r *knowledgeTagBatchChunkRepo) UpdateChunks(_ context.Context, chunks []*types.Chunk) error {
	r.updated = chunks
	return nil
}

func TestUpdateKnowledgeTagBatchSynchronizesDocumentChunks(t *testing.T) {
	knowledgeRepo := &knowledgeTagBatchKnowledgeRepo{knowledge: &types.Knowledge{
		ID: "knowledge-1", TenantID: 1, KnowledgeBaseID: "kb-1", TagID: "source-folder",
	}}
	chunkRepo := &knowledgeTagBatchChunkRepo{chunks: []*types.Chunk{
		{ID: "chunk-1", KnowledgeID: "knowledge-1", TagID: "source-folder"},
		{ID: "chunk-2", KnowledgeID: "knowledge-1", TagID: "source-folder"},
	}}
	service := &knowledgeService{
		repo:      knowledgeRepo,
		tagRepo:   knowledgeTagBatchTagRepo{tag: &types.KnowledgeTag{ID: "target-folder", KnowledgeBaseID: "kb-1"}},
		chunkRepo: chunkRepo,
	}
	targetTagID := "target-folder"
	ctx := context.WithValue(t.Context(), types.TenantIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, &types.Tenant{})

	err := service.UpdateKnowledgeTagBatch(ctx, "kb-1", map[string]*string{"knowledge-1": &targetTagID})

	require.NoError(t, err)
	require.Len(t, knowledgeRepo.updated, 1)
	require.Equal(t, targetTagID, knowledgeRepo.updated[0].TagID)
	require.Len(t, chunkRepo.updated, 2)
	require.Equal(t, targetTagID, chunkRepo.updated[0].TagID)
	require.Equal(t, targetTagID, chunkRepo.updated[1].TagID)
}
