package chatpipeline

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestGroupAndMergeOverlappingPreservesFirstSeenGroupOrder(t *testing.T) {
	results := []*types.SearchResult{
		{ID: "doc-b", KnowledgeID: "b", ChunkType: "text", StartAt: 0, EndAt: 1},
		{ID: "doc-a", KnowledgeID: "a", ChunkType: "text", StartAt: 0, EndAt: 1},
	}

	got := (&PluginMerge{}).groupAndMergeOverlapping(context.Background(), results)
	if len(got) != 2 || got[0].ID != "doc-b" || got[1].ID != "doc-a" {
		t.Fatalf("group order was not preserved: %+v", got)
	}
}
