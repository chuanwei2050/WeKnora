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

func TestApplyGraphSchemaFilter_StrictEnforcesConfiguredRelationDirection(t *testing.T) {
	graph := &types.GraphData{
		Node: []*types.GraphNode{{Name: "方法", EntityType: "method"}, {Name: "工具", EntityType: "tool"}},
		Relation: []*types.GraphRelation{
			{Node1: "方法", Node2: "工具", Type: "uses"},
			{Node1: "工具", Node2: "方法", Type: "uses"},
		},
	}
	result := ApplyGraphSchemaFilter(context.Background(), graph, SchemaFilterOptions{
		StrictSchema: true,
		Tags:         []string{"uses"},
		EntityTypes:  []string{"method", "tool"},
		RelationSchema: []types.GraphRelationTypeDefinition{
			{Type: "uses", SourceType: "method", TargetType: "tool", Description: "方法使用工具"},
		},
	})
	if result.SkipWrite || len(graph.Relation) != 1 || graph.Relation[0].Node1 != "方法" || graph.Relation[0].Node2 != "工具" {
		t.Fatalf("expected only configured direction, result=%#v relations=%#v", result, graph.Relation)
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

func TestParseGraph_StructuredItemsCarriesAliasesAndConfidence(t *testing.T) {
	f := NewFormater()
	raw := `{"items":[{"entity":"Alpha","entity_type":"tool","aliases":["A"]},{"entity":"Beta","entity_type":"service"},{"entity1":"Alpha","entity2":"Beta","relation":"uses","confidence":0.8}]}`
	graph, err := f.ParseGraph(context.Background(), raw)
	if err != nil {
		t.Fatalf("ParseGraph: %v", err)
	}
	if len(graph.Node) != 2 || len(graph.Node[0].Aliases) != 1 || graph.Node[0].Aliases[0] != "A" {
		t.Fatalf("aliases were not parsed: %#v", graph.Node)
	}
	if len(graph.Relation) != 1 || graph.Relation[0].Confidence != 0.8 {
		t.Fatalf("confidence was not parsed: %#v", graph.Relation)
	}
}

func TestParseGraph_DropsUnknownRelationshipEndpoint(t *testing.T) {
	f := NewFormater()
	raw := `{"items":[{"entity":"Alpha"},{"entity1":"Alpha","entity2":"Missing","relation":"uses","confidence":0.9}]}`
	graph, err := f.ParseGraph(context.Background(), raw)
	if err != nil {
		t.Fatalf("ParseGraph: %v", err)
	}
	if len(graph.Node) != 1 || len(graph.Relation) != 0 {
		t.Fatalf("unknown endpoint must not be materialized: %#v", graph)
	}
}

func TestApplyGraphExtractionPolicy_LimitsAndFilters(t *testing.T) {
	graph := &types.GraphData{
		Node: []*types.GraphNode{{Name: "A"}, {Name: "B"}, {Name: "C"}},
		Relation: []*types.GraphRelation{
			{Node1: "A", Node2: "B", Type: "uses", Confidence: 0.9},
			{Node1: "A", Node2: "C", Type: "uses", Confidence: 0.9},
			{Node1: "A", Node2: "B", Type: "weak", Confidence: 0.2},
		},
	}
	ApplyGraphExtractionPolicy(context.Background(), graph, 2, 1, 0.5)
	if len(graph.Node) != 2 || len(graph.Relation) != 1 || graph.Relation[0].Node2 != "B" {
		t.Fatalf("unexpected extraction policy result: %#v", graph)
	}
}

func TestApplyGraphSchemaFilter_NormalizesConfiguredCasing(t *testing.T) {
	graph := &types.GraphData{
		Node:     []*types.GraphNode{{Name: "A", EntityType: "TOOL"}, {Name: "B", EntityType: "service"}},
		Relation: []*types.GraphRelation{{Node1: "A", Node2: "B", Type: "USES"}},
	}
	result := ApplyGraphSchemaFilter(context.Background(), graph, SchemaFilterOptions{
		StrictSchema: true,
		Tags:         []string{"uses"},
		EntityTypes:  []string{"tool", "service"},
	})
	if result.SkipWrite || graph.Relation[0].Type != "uses" || graph.Node[0].EntityType != "tool" {
		t.Fatalf("schema casing was not normalized: result=%#v graph=%#v", result, graph)
	}
}

func TestExtractorFiltersSchemaBeforeApplyingEntityLimit(t *testing.T) {
	stub := &validationChatStub{content: `{"items":[
		{"entity":"invalid","entity_type":"unknown"},
		{"entity":"A","entity_type":"tool"},
		{"entity":"B","entity_type":"service"},
		{"entity1":"A","entity2":"B","relation":"uses","confidence":0.9}
	]}`}
	extractor := NewExtractor(stub, &types.PromptTemplateStructured{
		Tags:          []string{"uses"},
		EntityTypes:   []string{"tool", "service"},
		StrictSchema:  true,
		MaxEntities:   2,
		MaxRelations:  1,
		MinConfidence: 0.5,
	})
	graph, err := extractor.Extract(context.Background(), "A uses B")
	if err != nil {
		t.Fatalf("extract graph: %v", err)
	}
	if len(graph.Node) != 2 || len(graph.Relation) != 1 {
		t.Fatalf("valid triple was lost before limits: %#v", graph)
	}
}

func TestParseGraphDropsMalformedItems(t *testing.T) {
	f := NewFormater()
	raw := `{"items":[
		{"entity":"   "},
		{"entity":"A"},
		{"entity":"B"},
		{"entity1":"A","entity2":"B","relation":"   ","confidence":0.9},
		{"entity1":"A","entity2":"B","relation":"uses","confidence":"high"}
	]}`
	graph, err := f.ParseGraph(context.Background(), raw)
	if err != nil {
		t.Fatalf("ParseGraph: %v", err)
	}
	if len(graph.Node) != 2 || len(graph.Relation) != 1 || graph.Relation[0].Confidence != 0 {
		t.Fatalf("malformed graph items were not normalized safely: %#v", graph)
	}
	ApplyGraphExtractionPolicy(context.Background(), graph, 2, 1, 0.5)
	if len(graph.Relation) != 0 {
		t.Fatalf("invalid confidence must not pass extraction policy: %#v", graph.Relation)
	}
}
