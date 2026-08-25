package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestDataTableChunksPreserveKnowledgeFolder(t *testing.T) {
	service := &DataTableSummaryService{}
	resources := &extractionResources{knowledge: &types.Knowledge{
		ID: "doc-1", TenantID: 7, KnowledgeBaseID: "kb-1", TagID: "folder-1",
	}}

	chunks := service.buildChunks(resources, "summary", "columns")

	require.Len(t, chunks, 2)
	for _, chunk := range chunks {
		require.Equal(t, "folder-1", chunk.TagID)
	}
}
