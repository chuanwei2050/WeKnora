package chatpipeline

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestApplyGraphSchemaFilter_TagsNonEmptyDropsUnknownRelation(t *testing.T) {
	graph := &types.GraphData{
		Node:     []*types.GraphNode{{Name: "A", EntityType: "tool"}, {Name: "B", EntityType: "tool"}},
		Relation: []*types.GraphRelation{{Node1: "A", Node2: "B", Type: "related_to"}, {Node1: "A", Node2: "B", Type: "uses"}},
	}
	result := ApplyGraphSchemaFilter(context.Background(), graph, SchemaFilterOptions{Tags: []string{"uses"}})
	if result.SkipWrite {
		t.Fatal("expected writable when valid uses remains")
	}
	if len(graph.Relation) != 1 || graph.Relation[0].Type != "uses" {
		t.Fatalf("expected only uses kept, got %#v", graph.Relation)
	}
}

func TestApplyGraphSchemaFilter_EmptyTagsNonStrictKeepsRelations(t *testing.T) {
	graph := &types.GraphData{
		Relation: []*types.GraphRelation{{Node1: "A", Node2: "B", Type: "related_to"}},
	}
	result := ApplyGraphSchemaFilter(context.Background(), graph, SchemaFilterOptions{})
	if result.SkipWrite {
		t.Fatal("empty tags non-strict should not skip write solely due to filter")
	}
	if len(graph.Relation) != 1 {
		t.Fatalf("expected relations preserved, got %#v", graph.Relation)
	}
}

func TestApplyGraphSchemaFilter_StrictEmptyTagsSkipsWrite(t *testing.T) {
	graph := &types.GraphData{
		Node:     []*types.GraphNode{{Name: "A", EntityType: "tool"}},
		Relation: []*types.GraphRelation{{Node1: "A", Node2: "B", Type: "uses"}},
	}
	result := ApplyGraphSchemaFilter(context.Background(), graph, SchemaFilterOptions{
		StrictSchema: true,
		EntityTypes:  []string{"tool"},
	})
	if !result.SkipWrite {
		t.Fatal("strict with empty tags must skip write")
	}
	if len(graph.Relation) != 0 {
		t.Fatalf("expected relations cleared, got %#v", graph.Relation)
	}
}

func TestApplyGraphSchemaFilter_StrictEmptyEntityTypesSkipsWrite(t *testing.T) {
	graph := &types.GraphData{
		Relation: []*types.GraphRelation{{Node1: "A", Node2: "B", Type: "uses"}},
	}
	result := ApplyGraphSchemaFilter(context.Background(), graph, SchemaFilterOptions{
		StrictSchema: true,
		Tags:         []string{"uses"},
	})
	if !result.SkipWrite {
		t.Fatal("strict with empty entity_types must skip write")
	}
}

func TestApplyGraphSchemaFilter_StrictDropsUnknownOrEmptyEntityType(t *testing.T) {
	graph := &types.GraphData{
		Node: []*types.GraphNode{
			{Name: "A", EntityType: "tool"},
			{Name: "B", EntityType: "unknown_type"},
			{Name: "C"},
		},
		Relation: []*types.GraphRelation{
			{Node1: "A", Node2: "B", Type: "uses"},
			{Node1: "A", Node2: "C", Type: "uses"},
		},
	}
	result := ApplyGraphSchemaFilter(context.Background(), graph, SchemaFilterOptions{
		StrictSchema: true,
		Tags:         []string{"uses"},
		EntityTypes:  []string{"tool"},
	})
	if !result.SkipWrite {
		t.Fatal("expected skip write after all relations involve invalid entities")
	}
	if len(graph.Node) != 1 || graph.Node[0].Name != "A" {
		t.Fatalf("expected only tool node kept, got %#v", graph.Node)
	}
	if len(graph.Relation) != 0 {
		t.Fatalf("expected relations dropped, got %#v", graph.Relation)
	}
}

func TestApplyGraphSchemaFilter_StrictKeepsValidTriple(t *testing.T) {
	graph := &types.GraphData{
		Node:     []*types.GraphNode{{Name: "A", EntityType: "tool"}, {Name: "B", EntityType: "metric"}},
		Relation: []*types.GraphRelation{{Node1: "A", Node2: "B", Type: "uses"}},
	}
	result := ApplyGraphSchemaFilter(context.Background(), graph, SchemaFilterOptions{
		StrictSchema: true,
		Tags:         []string{"uses"},
		EntityTypes:  []string{"tool", "metric"},
	})
	if result.SkipWrite {
		t.Fatal("valid strict triple should be writable")
	}
	if len(graph.Relation) != 1 || len(graph.Node) != 2 {
		t.Fatalf("expected graph preserved, nodes=%#v relations=%#v", graph.Node, graph.Relation)
	}
}

func TestParseGraph_FillsEntityType(t *testing.T) {
	f := NewFormater()
	raw := "```json\n[{\"entity\":\"Alpha\",\"entity_type\":\"tool\",\"entity_attributes\":[\"x\"]},{\"entity1\":\"Alpha\",\"entity2\":\"Beta\",\"relation\":\"uses\"}]\n```"
	graph, err := f.ParseGraph(context.Background(), raw)
	if err != nil {
		t.Fatalf("ParseGraph: %v", err)
	}
	if len(graph.Node) == 0 || graph.Node[0].EntityType != "tool" {
		t.Fatalf("expected EntityType=tool, got %#v", graph.Node)
	}
}
