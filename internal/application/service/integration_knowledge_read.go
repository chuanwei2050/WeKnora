package service

import (
	"context"
	"fmt"
	"github.com/Tencent/WeKnora/internal/types"
)

type integrationVersionChunkPager interface {
	ListPagedChunksByKnowledgeVersionID(context.Context, uint64, string, string, *types.Pagination, []types.ChunkType, string, string, string, string, string) ([]*types.Chunk, int64, error)
}

// ReadIntegrationKnowledgeChunks reads only the authorized published revision in document order.
func (s *knowledgeService) ReadIntegrationKnowledgeChunks(ctx context.Context, tenantID uint64, knowledgeID, versionID string, page, pageSize int) ([]*types.Chunk, int64, error) {
	pagination := &types.Pagination{Page: page, PageSize: pageSize}
	if versionID != "" {
		pager, ok := s.chunkRepo.(integrationVersionChunkPager)
		if !ok {
			return nil, 0, fmt.Errorf("published version pagination unavailable")
		}
		return pager.ListPagedChunksByKnowledgeVersionID(ctx, tenantID, knowledgeID, versionID, pagination, []types.ChunkType{types.ChunkTypeText}, "", "", "", "asc", "manual")
	}
	return s.chunkRepo.ListPagedChunksByKnowledgeID(ctx, tenantID, knowledgeID, pagination, []types.ChunkType{types.ChunkTypeText}, "", "", "", "asc", "manual")
}
