package chatpipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type graphRepositoryFixture struct {
	result    *types.GraphSearchResult
	legacy    *types.GraphData
	lastQuery types.GraphQuery
}

func (f *graphRepositoryFixture) AddGraph(context.Context, types.NameSpace, []*types.GraphData) error {
	return nil
}

func (f *graphRepositoryFixture) DelGraph(context.Context, []types.NameSpace) error { return nil }

func (f *graphRepositoryFixture) SearchNode(context.Context, types.NameSpace, []string) (*types.GraphData, error) {
	return f.legacy, nil
}

func (f *graphRepositoryFixture) SearchPaths(_ context.Context, query types.GraphQuery) (*types.GraphSearchResult, error) {
	f.lastQuery = query
	return f.result, nil
}

func (f *graphRepositoryFixture) EnsureCanonicalSchema(context.Context) error { return nil }

func (f *graphRepositoryFixture) UpsertCanonicalRecords(context.Context, uint64, string, string, []types.GraphRebuildRecord) error {
	return nil
}

func (f *graphRepositoryFixture) ReplaceCanonicalSourceRecords(context.Context, uint64, string, string, types.GraphSource, []types.GraphRebuildRecord) error {
	return nil
}

func (f *graphRepositoryFixture) RemoveCanonicalSource(context.Context, uint64, string, string, types.GraphSource) error {
	return nil
}

func (f *graphRepositoryFixture) DeleteCanonicalKnowledgeBase(context.Context, uint64, string) error {
	return nil
}

func (f *graphRepositoryFixture) RebuildCanonicalGraph(context.Context, uint64, string, string, []types.GraphRebuildRecord, bool) (types.GraphRebuildResult, error) {
	return types.GraphRebuildResult{}, nil
}

func (f *graphRepositoryFixture) SwitchCanonicalNamespace(context.Context, uint64, string, string) error {
	return nil
}

func (f *graphRepositoryFixture) RollbackCanonicalNamespace(context.Context, uint64, string) (string, error) {
	return "", nil
}

var _ interfaces.RetrieveGraphRepository = (*graphRepositoryFixture)(nil)

type graphKnowledgeServiceFixture struct {
	interfaces.KnowledgeBaseService
	kb *types.KnowledgeBase
}

func (f graphKnowledgeServiceFixture) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return f.kb, nil
}

func TestPluginSearchEntityReturnsTypedGraphAndPreservesScope(t *testing.T) {
	fixture := &graphRepositoryFixture{result: &types.GraphSearchResult{
		Nodes: []types.CanonicalEntity{{CanonicalKey: "1:kb:tool:alpha", Name: "Alpha", EntityType: "tool"}},
		Edges: []types.GraphEdge{{ID: "edge-1", Source: "1:kb:tool:alpha", Target: "1:kb:tool:beta", RelationType: "uses", Direction: types.GraphDirectionOutgoing, Evidence: []types.GraphEvidence{{KnowledgeID: "doc-1", ChunkID: "chunk-1"}}}},
		Paths: []types.GraphPath{{NodeKeys: []string{"1:kb:tool:alpha", "1:kb:tool:beta"}, Edges: []types.GraphEdge{{ID: "edge-1", Source: "1:kb:tool:alpha", Target: "1:kb:tool:beta", RelationType: "uses", Evidence: []types.GraphEvidence{{KnowledgeID: "doc-1", ChunkID: "chunk-1"}}}}, Evidence: []types.GraphEvidence{{KnowledgeID: "doc-1", ChunkID: "chunk-1"}}}},
	}}
	plugin := &PluginSearchEntity{graphRepo: fixture}
	chat := &types.ChatManage{PipelineRequest: types.PipelineRequest{TenantID: 7}}
	graph, result, err := plugin.searchPaths(context.Background(), chat, "kb", []string{"doc-1"}, []string{"Alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Fallback || len(result.Paths) != 1 || len(result.Citations) != 1 {
		t.Fatalf("unexpected typed graph result: %+v", result)
	}
	if graph == nil || len(graph.Node) != 1 || len(graph.Relation) != 1 {
		t.Fatalf("unexpected graph data: %+v", graph)
	}
	if fixture.lastQuery.Scope.TenantID != 7 || fixture.lastQuery.Scope.KnowledgeBaseID != "kb" || len(fixture.lastQuery.Scope.AllowedKnowledgeIDs) != 1 {
		t.Fatalf("graph scope was not preserved: %+v", fixture.lastQuery.Scope)
	}
}

func TestPluginSearchEntityConvertsLegacyGraphToExplicitFallback(t *testing.T) {
	fixture := &graphRepositoryFixture{
		result: &types.GraphSearchResult{Fallback: true, FallbackReason: "legacy_graph_schema"},
		legacy: &types.GraphData{
			Node:     []*types.GraphNode{{Name: "Alpha", Chunks: []string{"chunk-1"}}, {Name: "Beta", Chunks: []string{"chunk-2"}}},
			Relation: []*types.GraphRelation{{Node1: "Alpha", Node2: "Beta", Type: "uses"}},
		},
	}
	plugin := &PluginSearchEntity{graphRepo: fixture}
	graph, result, err := plugin.searchPaths(context.Background(), &types.ChatManage{PipelineRequest: types.PipelineRequest{TenantID: 7}}, "kb", []string{"doc-1"}, []string{"Alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.Fallback || result.FallbackReason != "legacy_graph_schema" {
		t.Fatalf("expected explicit legacy fallback: %+v", result)
	}
	if graph == nil || len(graph.Node) != 0 || len(graph.Relation) != 0 {
		t.Fatalf("legacy graph must not be converted into typed graph data: %+v", graph)
	}
}

type graphGovernanceKnowledgeFixture struct {
	interfaces.KnowledgeRepository
	items []*types.Knowledge
}

func (f graphGovernanceKnowledgeFixture) GetKnowledgeBatch(context.Context, uint64, []string) ([]*types.Knowledge, error) {
	return f.items, nil
}

type graphGovernanceVersionFixture struct {
	interfaces.KnowledgeGovernanceRepository
	version *types.KnowledgeVersion
}

func (f graphGovernanceVersionFixture) GetVersion(context.Context, uint64, string) (*types.KnowledgeVersion, error) {
	return f.version, nil
}

func TestFilterGovernedGraphResultBeforeRendering(t *testing.T) {
	plugin := &PluginSearchEntity{knowledgeRepo: graphGovernanceKnowledgeFixture{items: []*types.Knowledge{{ID: "doc", CurrentVersionID: "current"}}}}
	result := &types.GraphSearchResult{
		Nodes: []types.CanonicalEntity{{CanonicalKey: "a"}, {CanonicalKey: "stale"}},
		Edges: []types.GraphEdge{{ID: "edge", Source: "a", Target: "stale", Evidence: []types.GraphEvidence{{KnowledgeID: "doc", KnowledgeVersionID: "old", ChunkID: "old-chunk"}}}},
		Paths: []types.GraphPath{{NodeKeys: []string{"a", "stale"}, Edges: []types.GraphEdge{{ID: "edge", Evidence: []types.GraphEvidence{{KnowledgeID: "doc", KnowledgeVersionID: "old", ChunkID: "old-chunk"}}}}, Evidence: []types.GraphEvidence{{KnowledgeID: "doc", KnowledgeVersionID: "old", ChunkID: "old-chunk"}}}},
	}
	filtered := plugin.filterGovernedGraphResult(context.Background(), &types.ChatManage{PipelineRequest: types.PipelineRequest{TenantID: 7}}, result)
	if filtered == nil || len(filtered.Paths) != 0 || len(filtered.Edges) != 0 || len(filtered.Citations) != 0 {
		t.Fatalf("stale graph evidence leaked: %+v", filtered)
	}
}

func TestFilterGovernedGraphResultPreservesCurrentPathEdges(t *testing.T) {
	plugin := &PluginSearchEntity{knowledgeRepo: graphGovernanceKnowledgeFixture{items: []*types.Knowledge{{ID: "doc", CurrentVersionID: "current"}}}}
	evidence := types.GraphEvidence{KnowledgeID: "doc", KnowledgeVersionID: "current", ChunkID: "chunk"}
	result := &types.GraphSearchResult{
		Nodes: []types.CanonicalEntity{{CanonicalKey: "a"}, {CanonicalKey: "b"}},
		Edges: []types.GraphEdge{{ID: "edge", Source: "a", Target: "b", Evidence: []types.GraphEvidence{evidence}}},
		Paths: []types.GraphPath{{NodeKeys: []string{"a", "b"}, Edges: []types.GraphEdge{{ID: "edge", Evidence: []types.GraphEvidence{evidence}}}, Evidence: []types.GraphEvidence{evidence}}},
	}
	filtered := plugin.filterGovernedGraphResult(context.Background(), &types.ChatManage{PipelineRequest: types.PipelineRequest{TenantID: 7}}, result)
	if filtered == nil || len(filtered.Paths) != 1 || len(filtered.Paths[0].Edges) != 1 || len(filtered.Nodes) != 2 {
		t.Fatalf("current graph path was dropped: %+v", filtered)
	}
}

func TestFilterGovernedGraphResultRejectsNonRetrievableCurrentVersion(t *testing.T) {
	plugin := &PluginSearchEntity{
		knowledgeRepo:  graphGovernanceKnowledgeFixture{items: []*types.Knowledge{{ID: "doc", CurrentVersionID: "current"}}},
		governanceRepo: graphGovernanceVersionFixture{version: &types.KnowledgeVersion{ID: "current", Status: types.KnowledgeVersionDraft}},
	}
	result := &types.GraphSearchResult{
		Citations: []types.GraphEvidence{{KnowledgeID: "doc", KnowledgeVersionID: "current", ChunkID: "chunk"}},
	}
	filtered := plugin.filterGovernedGraphResult(context.Background(), &types.ChatManage{PipelineRequest: types.PipelineRequest{TenantID: 7}}, result)
	if filtered == nil || len(filtered.Citations) != 0 {
		t.Fatalf("non-retrievable current version leaked: %+v", filtered)
	}
}

func TestFilterGovernedGraphResultRejectsGraphWithoutEvidence(t *testing.T) {
	plugin := &PluginSearchEntity{knowledgeRepo: graphGovernanceKnowledgeFixture{items: []*types.Knowledge{{ID: "doc", CurrentVersionID: "current"}}}}
	result := &types.GraphSearchResult{Nodes: []types.CanonicalEntity{{CanonicalKey: "orphan", Name: "Orphan"}}}
	filtered := plugin.filterGovernedGraphResult(context.Background(), &types.ChatManage{PipelineRequest: types.PipelineRequest{TenantID: 7}}, result)
	if filtered == nil || !filtered.Fallback || filtered.FallbackReason != "graph_evidence_missing" {
		t.Fatalf("graph without evidence must fail closed: %+v", filtered)
	}
}

func TestMergeGraphSearchResultsPreservesDirectCitations(t *testing.T) {
	result := mergeGraphSearchResults([]*types.GraphSearchResult{{
		Citations: []types.GraphEvidence{{KnowledgeID: "doc", KnowledgeVersionID: "v1", ChunkID: "chunk"}},
	}})
	if result == nil || len(result.Citations) != 1 || result.Citations[0].ChunkID != "chunk" {
		t.Fatalf("direct graph citation was dropped: %+v", result)
	}
}

func TestQueryKnowledgeGraphToolReturnsTypedGraphData(t *testing.T) {
	fixture := &graphRepositoryFixture{result: &types.GraphSearchResult{
		Nodes: []types.CanonicalEntity{{CanonicalKey: "1:kb:tool:alpha", Name: "Alpha", EntityType: "tool"}},
		Edges: []types.GraphEdge{{ID: "edge-1", Source: "1:kb:tool:alpha", Target: "1:kb:tool:beta", RelationType: "uses", Evidence: []types.GraphEvidence{{KnowledgeID: "doc-1", ChunkID: "chunk-1"}}}},
		Paths: []types.GraphPath{{NodeKeys: []string{"1:kb:tool:alpha", "1:kb:tool:beta"}, Evidence: []types.GraphEvidence{{KnowledgeID: "doc-1", ChunkID: "chunk-1"}}}},
	}}
	tool := tools.NewQueryKnowledgeGraphTool(graphKnowledgeServiceFixture{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1, IndexingStrategy: types.IndexingStrategy{GraphEnabled: true}, ExtractConfig: &types.ExtractConfig{Enabled: true, Tags: []string{"uses"}, Relations: []*types.GraphRelation{{Type: "few-shot-only"}}}}}, fixture, nil, nil, types.SearchTargets{&types.SearchTarget{KnowledgeBaseID: "kb", Type: types.SearchTargetTypeKnowledgeBase, TenantID: 1}})
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	result, err := tool.Execute(ctx, []byte(`{"knowledge_base_ids":["kb"],"query":"Alpha"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.Success {
		t.Fatalf("typed graph tool failed: %+v", result)
	}
	data, ok := result.Data["graph_data"].(*types.GraphSearchResult)
	if !ok || len(data.Edges) != 1 || len(data.Citations) != 1 {
		t.Fatalf("tool did not return structured graph data: %#v", result.Data["graph_data"])
	}
}

func TestQueryKnowledgeGraphToolRejectsUnauthorizedTargetsBeforeFallback(t *testing.T) {
	tool := tools.NewQueryKnowledgeGraphTool(nil, nil, nil, nil, types.SearchTargets{
		&types.SearchTarget{KnowledgeBaseID: "authorized", Type: types.SearchTargetTypeKnowledgeBase, TenantID: 1},
	})
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	result, err := tool.Execute(ctx, []byte(`{"knowledge_base_ids":["other"],"query":"Alpha"}`))
	if err == nil || result == nil || result.Success || !strings.Contains(result.Error, "authorized search scope") {
		t.Fatalf("unauthorized graph fallback target was not rejected: result=%+v err=%v", result, err)
	}
}
