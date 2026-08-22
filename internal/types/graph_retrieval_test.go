package types

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestTraverseGraphBoundsAndEvidenceScope(t *testing.T) {
	nodes := []CanonicalEntity{
		{CanonicalKey: "a", Name: "A", EntityType: "tool", TenantID: 1, KnowledgeBaseID: "kb"},
		{CanonicalKey: "b", Name: "B", EntityType: "tool", TenantID: 1, KnowledgeBaseID: "kb"},
		{CanonicalKey: "c", Name: "C", EntityType: "tool", TenantID: 1, KnowledgeBaseID: "kb"},
	}
	edges := []GraphEdge{
		{ID: "ab", Source: "a", Target: "b", RelationType: "uses", Weight: 1, Evidence: []GraphEvidence{{ChunkID: "c1", KnowledgeID: "k1"}}},
		{ID: "bc", Source: "b", Target: "c", RelationType: "uses", Weight: .8, Evidence: []GraphEvidence{{ChunkID: "c2", KnowledgeID: "k2"}}},
		{ID: "ca", Source: "c", Target: "a", RelationType: "uses", Weight: .9, Evidence: []GraphEvidence{{ChunkID: "c3", KnowledgeID: "k1"}}},
	}
	result, err := TraverseGraph(context.Background(), GraphQuery{
		Scope: GraphScope{TenantID: 1, KnowledgeBaseID: "kb", AllowedKnowledgeIDs: []string{"k1"}},
		Seeds: []GraphSeed{{Name: "a"}}, MaxDepth: 3, BranchFactor: 1, MaxPaths: 10, Timeout: time.Second,
	}, nodes, edges, []string{"uses"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Paths) != 1 {
		t.Fatalf("expected one authorized path, got %d", len(result.Paths))
	}
	if len(result.Paths[0].Evidence) != 1 || result.Paths[0].Evidence[0].KnowledgeID != "k1" {
		t.Fatalf("unexpected evidence: %#v", result.Paths[0].Evidence)
	}
	if len(result.Edges) != 1 || result.Edges[0].ID != "ab" {
		t.Fatalf("unexpected result edges: %#v", result.Edges)
	}
	if result.Paths[0].NodeKeys[0] != "a" || result.Paths[0].NodeKeys[1] != "b" {
		t.Fatalf("unexpected path: %#v", result.Paths[0].NodeKeys)
	}
}

func TestGraphQueryRejectsUnknownRelationAndInvalidScope(t *testing.T) {
	query := GraphQuery{Scope: GraphScope{TenantID: 1, KnowledgeBaseID: "kb"}, Seeds: []GraphSeed{{Name: "a"}}, RelationTypes: []string{"delete"}}
	if err := query.Validate([]string{"uses"}); err == nil {
		t.Fatal("expected unknown relation to be rejected")
	}
	query.Scope.TenantID = 0
	query.RelationTypes = nil
	if err := query.Validate([]string{"uses"}); err == nil {
		t.Fatal("expected missing tenant to be rejected")
	}
}

func TestTraverseGraphFiltersMixedGovernedVersions(t *testing.T) {
	nodes := []CanonicalEntity{
		{CanonicalKey: "a", Name: "A", EntityType: "tool"},
		{CanonicalKey: "b", Name: "B", EntityType: "tool"},
	}
	edges := []GraphEdge{{
		ID: "ab", Source: "a", Target: "b", RelationType: "uses", Direction: GraphDirectionOutgoing,
		Evidence: []GraphEvidence{
			{ChunkID: "current", KnowledgeID: "doc", KnowledgeVersionID: "v-current"},
			{ChunkID: "draft", KnowledgeID: "doc", KnowledgeVersionID: "v-draft"},
		},
	}}
	result, err := TraverseGraph(context.Background(), GraphQuery{
		Scope: GraphScope{TenantID: 1, KnowledgeBaseID: "kb", AllowedKnowledgeIDs: []string{"doc"}, CurrentKnowledgeVersions: map[string]string{"doc": "v-current"}},
		Seeds: []GraphSeed{{Name: "A"}}, RelationTypes: []string{"uses"}, Direction: GraphDirectionOutgoing,
	}, nodes, edges, []string{"uses"})
	if err != nil || len(result.Paths) != 1 || len(result.Paths[0].Evidence) != 1 || result.Paths[0].Evidence[0].KnowledgeVersionID != "v-current" {
		t.Fatalf("mixed version evidence was not filtered: result=%+v err=%v", result, err)
	}
}

func TestGraphNamespacePublicationRequiresAllIndexes(t *testing.T) {
	store := NewGraphStoreWithRelations([]string{"tool"}, nil, []string{"uses"})
	if _, err := store.RebuildFromRecords(context.Background(), 1, "kb", "staging:v2", []GraphRebuildRecord{{Entity: &CanonicalEntity{Name: "A", EntityType: "tool"}}}, false); err != nil {
		t.Fatal(err)
	}
	if err := store.PublishNamespace(1, "kb", "staging:v2", "active:v2", GraphPublicationReadiness{VectorReady: true, KeywordReady: true, GraphReady: true}); err == nil {
		t.Fatal("expected publication to reject incomplete readiness")
	}
	if err := store.PublishNamespace(1, "kb", "staging:v2", "active:v2", GraphPublicationReadiness{VectorReady: true, KeywordReady: true, GraphReady: true, EvidenceVersionsValid: true}); err != nil {
		t.Fatal(err)
	}
	if store.ActiveNamespace(1, "kb") != "active:v2" {
		t.Fatalf("expected active namespace switch, got %q", store.ActiveNamespace(1, "kb"))
	}
}

func TestTraverseGraphRejectsUnauthorizedEvidenceAndHonorsExpansionBudget(t *testing.T) {
	nodes := []CanonicalEntity{
		{CanonicalKey: "a", Name: "A", EntityType: "tool"},
		{CanonicalKey: "b", Name: "B", EntityType: "tool"},
	}
	edges := []GraphEdge{{
		ID: "ab", Source: "a", Target: "b", RelationType: "uses", Direction: GraphDirectionOutgoing,
		Evidence: []GraphEvidence{{ChunkID: "unauthorized", KnowledgeID: "other-tenant-doc"}},
	}}
	unauthorized, err := TraverseGraph(context.Background(), GraphQuery{
		Scope: GraphScope{TenantID: 1, KnowledgeBaseID: "kb", AllowedKnowledgeIDs: []string{"allowed-doc"}},
		Seeds: []GraphSeed{{Name: "A"}}, RelationTypes: []string{"uses"}, MaxDepth: 2,
	}, nodes, edges, []string{"uses"})
	if err != nil {
		t.Fatal(err)
	}
	if len(unauthorized.Paths) != 0 {
		t.Fatalf("an edge supported only by unauthorized evidence must be hidden: %#v", unauthorized.Paths)
	}

	bounded, err := TraverseGraph(context.Background(), GraphQuery{
		Scope: GraphScope{TenantID: 1, KnowledgeBaseID: "kb", AllowedKnowledgeIDs: []string{"other-tenant-doc"}},
		Seeds: []GraphSeed{{Name: "A"}}, RelationTypes: []string{"uses"}, MaxDepth: 2, MaxExpandedNodes: 1,
	}, nodes, edges, []string{"uses"})
	if err != nil {
		t.Fatal(err)
	}
	if !bounded.Truncated || bounded.TruncationReason != "expanded_nodes" {
		t.Fatalf("expected expansion budget truncation, got %#v", bounded)
	}
}

func TestFuseGraphAndTextScoresIsDeterministic(t *testing.T) {
	items, err := FuseGraphAndTextScores(GraphSearchResult{Paths: []GraphPath{{NodeKeys: []string{"a", "b"}, Score: .8, Evidence: []GraphEvidence{{ChunkID: "c1", KnowledgeID: "k1"}}}}}, []*SearchResult{{ID: "c1", Score: .4}}, GraphTextFusionConfig{GraphWeight: .75, TextWeight: .25, MaxResults: 10})
	if err != nil || len(items) != 1 {
		t.Fatalf("unexpected fused result: %#v err=%v", items, err)
	}
	if items[0].Score != .7 {
		t.Fatalf("unexpected score: %v", items[0].Score)
	}
}

func TestFuseGraphAndTextScoresKeepsTextOnlyEvidence(t *testing.T) {
	items, err := FuseGraphAndTextScores(GraphSearchResult{}, []*SearchResult{{ID: "text-only", KnowledgeID: "k1", Score: .6}}, GraphTextFusionConfig{GraphWeight: .5, TextWeight: .5, MaxResults: 10})
	if err != nil || len(items) != 1 || items[0].Evidence.ChunkID != "text-only" || items[0].Score != .3 {
		t.Fatalf("unexpected text-only fusion: %#v err=%v", items, err)
	}
}

func TestRenderGraphContextKeepsEvidenceLinksWithinBudget(t *testing.T) {
	value := RenderGraphContext(&GraphSearchResult{Paths: []GraphPath{{NodeKeys: []string{"a", "b"}, Score: .8, Edges: []GraphEdge{{Source: "a", Target: "b", RelationType: "uses"}}, Evidence: []GraphEvidence{{KnowledgeID: "k1", ChunkID: "c1", KnowledgeVersionID: "v1"}}}}}, 400)
	if value == "" || !strings.Contains(value, "chunk_id=\"c1\"") || len([]rune(value)) > 430 {
		t.Fatalf("unexpected graph context: %q", value)
	}
}
