package neo4j

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
)

func TestNormalizeGraphAliases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  []string
		expect []string
	}{
		{
			name:   "dedupes and normalizes",
			input:  []string{" Foo ", "foo", "BAR", "bar"},
			expect: []string{"foo", "bar"},
		},
		{
			name:   "drops empty",
			input:  []string{"", "  ", "valid"},
			expect: []string{"valid"},
		},
		{
			name:   "nil input",
			input:  nil,
			expect: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeGraphAliases(tt.input)
			if len(got) != len(tt.expect) {
				t.Fatalf("normalizeGraphAliases() len = %d, want %d (%v)", len(got), len(tt.expect), got)
			}
			for i := range tt.expect {
				if got[i] != tt.expect[i] {
					t.Fatalf("normalizeGraphAliases()[%d] = %q, want %q", i, got[i], tt.expect[i])
				}
			}
		})
	}
}

func TestNormalizedRelationTypes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  []string
		expect []string
	}{
		{
			name:   "lowercase trim dedupe",
			input:  []string{" Uses ", "USES", "depends_on", ""},
			expect: []string{"uses", "depends_on"},
		},
		{
			name:   "empty",
			input:  []string{},
			expect: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizedRelationTypes(tt.input)
			if len(got) != len(tt.expect) {
				t.Fatalf("normalizedRelationTypes() = %v, want %v", got, tt.expect)
			}
			for i := range tt.expect {
				if got[i] != tt.expect[i] {
					t.Fatalf("normalizedRelationTypes()[%d] = %q, want %q", i, got[i], tt.expect[i])
				}
			}
		})
	}
}

func TestGraphSourceEvidenceKeyMatchesGraphEvidenceIdentity(t *testing.T) {
	t.Parallel()
	source := types.GraphSource{
		KnowledgeID:        "k1",
		KnowledgeVersionID: "v2",
		ChunkID:            "c3",
		ExtractorID:        "ext4",
	}
	evidence := types.GraphEvidence{
		KnowledgeID:        source.KnowledgeID,
		KnowledgeVersionID: source.KnowledgeVersionID,
		ChunkID:            source.ChunkID,
		ExtractorID:        source.ExtractorID,
	}
	want := "k1|v2|c3|ext4"
	if got := graphSourceEvidenceKey(source); got != want {
		t.Fatalf("graphSourceEvidenceKey() = %q, want %q", got, want)
	}
	if got := graphEvidenceIdentity(evidence); got != want {
		t.Fatalf("graphEvidenceIdentity() = %q, want %q", got, want)
	}
}

func TestGraphEdgeTargetForQuery(t *testing.T) {
	t.Parallel()
	edge := types.GraphEdge{Source: "a", Target: "b"}

	tests := []struct {
		name      string
		current   string
		direction types.GraphDirection
		want      string
		ok        bool
	}{
		{"outgoing match", "a", types.GraphDirectionOutgoing, "b", true},
		{"outgoing miss", "b", types.GraphDirectionOutgoing, "b", false},
		{"incoming match", "b", types.GraphDirectionIncoming, "a", true},
		{"both from source", "a", types.GraphDirectionBoth, "b", true},
		{"both from target", "b", types.GraphDirectionBoth, "a", true},
		{"both unrelated", "c", types.GraphDirectionBoth, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := graphEdgeTargetForQuery(edge, tt.current, tt.direction)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("graphEdgeTargetForQuery() = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestMergeCanonicalEdgeAndAppendUniqueGraphEvidence(t *testing.T) {
	t.Parallel()
	evidenceA := types.GraphEvidence{KnowledgeID: "k1", ChunkID: "c1"}
	evidenceB := types.GraphEvidence{KnowledgeID: "k2", ChunkID: "c2"}

	merged := mergeCanonicalEdge(types.GraphEdge{}, types.GraphEdge{
		ID:       "rel1",
		Evidence: []types.GraphEvidence{evidenceA},
	})
	if merged.ID != "rel1" || len(merged.Evidence) != 1 {
		t.Fatalf("merge into empty edge = %+v", merged)
	}

	merged = mergeCanonicalEdge(merged, types.GraphEdge{
		ID:       "rel1",
		Evidence: []types.GraphEvidence{evidenceA, evidenceB},
	})
	if len(merged.Evidence) != 2 {
		t.Fatalf("expected deduped merge to keep two evidences, got %d", len(merged.Evidence))
	}

	merged = mergeCanonicalEdge(merged, types.GraphEdge{
		ID:       "rel1",
		Evidence: []types.GraphEvidence{evidenceA},
	})
	if len(merged.Evidence) != 2 {
		t.Fatalf("duplicate evidence should not grow slice, got %d", len(merged.Evidence))
	}
}

func TestUniqueStrings(t *testing.T) {
	t.Parallel()
	got := uniqueStrings([]string{"b", "a", "b", "c", "a"})
	want := []string{"b", "a", "c"}
	if len(got) != len(want) {
		t.Fatalf("uniqueStrings() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("uniqueStrings()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if short := uniqueStrings([]string{"only"}); len(short) != 1 || short[0] != "only" {
		t.Fatalf("uniqueStrings(single) = %v", short)
	}
}

func TestMapCanonicalEntitiesAndEdgesSortByKey(t *testing.T) {
	t.Parallel()
	entities := map[string]types.CanonicalEntity{
		"z": {CanonicalKey: "z"},
		"a": {CanonicalKey: "a"},
	}
	sortedEntities := mapCanonicalEntities(entities)
	if len(sortedEntities) != 2 || sortedEntities[0].CanonicalKey != "a" || sortedEntities[1].CanonicalKey != "z" {
		t.Fatalf("mapCanonicalEntities() = %+v", sortedEntities)
	}

	edges := map[string]types.GraphEdge{
		"b": {ID: "b"},
		"a": {ID: "a"},
	}
	sortedEdges := mapCanonicalEdges(edges)
	if len(sortedEdges) != 2 || sortedEdges[0].ID != "a" || sortedEdges[1].ID != "b" {
		t.Fatalf("mapCanonicalEdges() = %+v", sortedEdges)
	}
}

func TestNeo4jValueCoercions(t *testing.T) {
	t.Parallel()
	node := neo4j.Node{Props: map[string]interface{}{"k": "v"}}
	if got, ok := neo4jNode(node); !ok || got.Props["k"] != "v" {
		t.Fatalf("neo4jNode(concrete) = (%+v, %v)", got, ok)
	}
	ptr := &node
	if got, ok := neo4jNode(ptr); !ok || got.Props["k"] != "v" {
		t.Fatalf("neo4jNode(pointer) = (%+v, %v)", got, ok)
	}
	if _, ok := neo4jNode(nil); ok {
		t.Fatal("neo4jNode(nil) should be false")
	}

	rel := neo4j.Relationship{Props: map[string]interface{}{"relation_key": "rk"}}
	if got, ok := neo4jRelationship(rel); !ok || got.Props["relation_key"] != "rk" {
		t.Fatalf("neo4jRelationship(concrete) = (%+v, %v)", got, ok)
	}
	if list := neo4jList([]interface{}{"a", 1}); len(list) != 2 {
		t.Fatalf("neo4jList() = %v", list)
	}
	if neo4jList("not-a-list") != nil {
		t.Fatal("neo4jList(non-list) should be nil")
	}
}

func TestGraphPropertyReaders(t *testing.T) {
	t.Parallel()
	props := map[string]interface{}{
		"name":    "entity",
		"weight":  int64(3),
		"aliases": []interface{}{" alias ", "", "<nil>"},
	}
	if got := stringGraphProperty(props, "name"); got != "entity" {
		t.Fatalf("stringGraphProperty(name) = %q", got)
	}
	if got := stringGraphProperty(props, "missing"); got != "" {
		t.Fatalf("stringGraphProperty(missing) = %q", got)
	}
	if got := floatGraphProperty(props, "weight"); got != 3 {
		t.Fatalf("floatGraphProperty(weight) = %v", got)
	}
	if got := stringListGraphProperty(props, "aliases"); len(got) != 1 || got[0] != "alias" {
		t.Fatalf("stringListGraphProperty(aliases) = %v", got)
	}
}

func TestCanonicalEntityFromNode(t *testing.T) {
	t.Parallel()
	node := neo4j.Node{Props: map[string]interface{}{
		"canonical_key":     "1:kb:tool:foo",
		"name":              "Foo",
		"entity_type":       "tool",
		"tenant_id":         int64(1),
		"knowledge_base_id": "kb",
		"aliases":           []interface{}{"bar"},
	}}
	entity, ok := canonicalEntityFromNode(node)
	if !ok || entity.CanonicalKey != "1:kb:tool:foo" || entity.Name != "Foo" || entity.TenantID != 1 {
		t.Fatalf("canonicalEntityFromNode() = (%+v, %v)", entity, ok)
	}
	if _, ok := canonicalEntityFromNode(neo4j.Node{Props: map[string]interface{}{}}); ok {
		t.Fatal("missing canonical_key should not parse")
	}
}

func TestCanonicalEdgeFromRelationship(t *testing.T) {
	t.Parallel()
	source := neo4j.Node{Props: map[string]interface{}{"canonical_key": "a"}}
	target := neo4j.Node{Props: map[string]interface{}{"canonical_key": "b"}}
	relationship := neo4j.Relationship{
		Type: "CONNECTS",
		Props: map[string]interface{}{
			"relation_key":  "rel-key",
			"source":        "a",
			"target":        "b",
			"relation_type": "uses",
			"direction":     string(types.GraphDirectionOutgoing),
			"weight":        2.5,
		},
	}
	edge := canonicalEdgeFromRelationship(relationship, source, target)
	if edge.ID != "rel-key" || edge.Source != "a" || edge.Target != "b" || edge.RelationType != "uses" || edge.Weight != 2.5 {
		t.Fatalf("canonicalEdgeFromRelationship() = %+v", edge)
	}

	fallbackRel := neo4j.Relationship{Type: "CONNECTS", Props: map[string]interface{}{"source": "a", "target": "b"}}
	fallbackEdge := canonicalEdgeFromRelationship(fallbackRel, source, target)
	if fallbackEdge.ID != "a|CONNECTS|b" || fallbackEdge.Direction != types.GraphDirectionOutgoing {
		t.Fatalf("fallback edge identity = %+v", fallbackEdge)
	}
}

func TestCanonicalEdgeFromRelationNode(t *testing.T) {
	t.Parallel()
	source := neo4j.Node{Props: map[string]interface{}{"canonical_key": "a"}}
	target := neo4j.Node{Props: map[string]interface{}{"canonical_key": "b"}}
	relation := neo4j.Node{Props: map[string]interface{}{
		"relation_key":  "rel-key",
		"source":        "a",
		"target":        "b",
		"relation_type": "uses",
		"direction":     string(types.GraphDirectionOutgoing),
		"weight":        2.5,
	}}
	edge := canonicalEdgeFromRelationNode(relation, source, target)
	if edge.ID != "rel-key" || edge.Source != "a" || edge.Target != "b" || edge.RelationType != "uses" || edge.Weight != 2.5 {
		t.Fatalf("canonicalEdgeFromRelationNode() = %+v", edge)
	}
}

func TestGraphEvidenceFromNode(t *testing.T) {
	t.Parallel()
	node := neo4j.Node{Props: map[string]interface{}{
		"knowledge_id":         "k1",
		"knowledge_version_id": "v1",
		"chunk_id":             "c1",
		"extractor_id":         "e1",
		"document_title":       "Doc",
		"source":               "chunk",
		"weight":               float32(1.5),
	}}
	evidence := graphEvidenceFromNode(node)
	if evidence.KnowledgeID != "k1" || evidence.ChunkID != "c1" || evidence.DocumentTitle != "Doc" || evidence.Weight != 1.5 {
		t.Fatalf("graphEvidenceFromNode() = %+v", evidence)
	}
}
