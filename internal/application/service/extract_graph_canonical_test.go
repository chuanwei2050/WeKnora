package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestCanonicalRecordsFromExtractedGraphCarriesVersionEvidence(t *testing.T) {
	chunk := &types.Chunk{TenantID: 7, KnowledgeBaseID: "kb", KnowledgeID: "doc", KnowledgeVersionID: "v2", ID: "chunk"}
	graph := &types.GraphData{
		Node: []*types.GraphNode{{Name: "A", EntityType: "tool"}, {Name: "B", EntityType: "service"}},
		Relation: []*types.GraphRelation{{Node1: "A", Node2: "B", Type: "uses"}},
	}
	records := canonicalRecordsFromExtractedGraph(chunk, graph, "model")
	if len(records) != 5 {
		t.Fatalf("expected two entities, two instances and one edge, got %d", len(records))
	}
	for _, record := range records {
		if record.Source.KnowledgeID != "doc" || record.Source.KnowledgeVersionID != "v2" || record.Source.ChunkID != "chunk" {
			t.Fatalf("record lost source version: %+v", record)
		}
	}
	if got := types.GraphNamespaceForVersion(chunk.KnowledgeVersionID, true); got != "staging:v2" {
		t.Fatalf("unexpected staging namespace %q", got)
	}
	if records[4].Edge == nil || records[4].Edge.Source == "" || records[4].Edge.Target == "" {
		t.Fatalf("edge was not canonicalized: %+v", records[4])
	}
}
