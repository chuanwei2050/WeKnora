package types

import (
	"context"
	"testing"
)

func TestGraphStorePreservesSharedEvidenceAndIsolatesNamespaces(t *testing.T) {
	store := NewGraphStore([]string{"tool", "service"}, map[string]string{"工具": "tool"})
	if _, err := store.AddEntity(1, "kb", "default", CanonicalEntity{Name: "A", EntityType: "工具"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddEntity(1, "kb", "default", CanonicalEntity{Name: "B", EntityType: "service"}); err != nil {
		t.Fatal(err)
	}
	a := CanonicalEntityKey(1, "kb", "tool", "A")
	b := CanonicalEntityKey(1, "kb", "service", "B")
	first := GraphSource{KnowledgeID: "k1", KnowledgeVersionID: "v1", ChunkID: "c1", ExtractorID: "extractor"}
	second := GraphSource{KnowledgeID: "k2", KnowledgeVersionID: "v1", ChunkID: "c2", ExtractorID: "extractor"}
	edge := GraphEdge{Source: a, Target: b, RelationType: "uses", Direction: GraphDirectionOutgoing, Weight: 1}
	if err := store.AddRelationshipEvidence(1, "kb", "default", edge, first); err != nil {
		t.Fatal(err)
	}
	if err := store.AddRelationshipEvidence(1, "kb", "default", edge, second); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveSource(1, "kb", "default", first); err != nil {
		t.Fatal(err)
	}
	result, err := store.Query(context.Background(), GraphQuery{Scope: GraphScope{TenantID: 1, KnowledgeBaseID: "kb", AllowedKnowledgeIDs: []string{"k2"}}, Seeds: []GraphSeed{{Name: "A", EntityType: "tool"}}, RelationTypes: []string{"uses"}}, []string{"uses"})
	if err != nil || len(result.Paths) != 1 || len(result.Paths[0].Evidence) != 1 || result.Paths[0].Evidence[0].KnowledgeID != "k2" {
		t.Fatalf("unexpected graph result: %#v err=%v", result, err)
	}
	if store.ActiveNamespace(1, "kb") != "" {
		t.Fatal("uncommitted namespace must not become active")
	}
}

func TestGraphStoreRebuildRequiresAuthoritativeEvidence(t *testing.T) {
	store := NewGraphStore([]string{"tool"}, nil)
	entityA := CanonicalEntity{Name: "A", EntityType: "tool"}
	entityB := CanonicalEntity{Name: "B", EntityType: "tool"}
	a := CanonicalEntityKey(1, "kb", "tool", "A")
	b := CanonicalEntityKey(1, "kb", "tool", "B")
	edge := GraphEdge{ID: "e1", Source: a, Target: b, RelationType: "uses", Direction: GraphDirectionOutgoing}
	if _, err := store.Rebuild(context.Background(), 1, "kb", "rebuild-1", []CanonicalEntity{entityA, entityB}, []GraphEdge{edge}, nil, true); err == nil {
		t.Fatal("expected rebuild without authoritative source to fail")
	}
	source := GraphSource{KnowledgeID: "k", ChunkID: "c", ExtractorID: "extractor"}
	result, err := store.Rebuild(context.Background(), 1, "kb", "rebuild-1", []CanonicalEntity{entityA, entityB}, []GraphEdge{edge}, map[string]GraphSource{"e1": source}, true)
	if err != nil || !result.Switched || store.ActiveNamespace(1, "kb") != "rebuild-1" {
		t.Fatalf("unexpected rebuild: %#v err=%v", result, err)
	}
}

func TestGraphStoreRejectsUnknownRelationsAndSeparatesEntityTypes(t *testing.T) {
	store := NewGraphStoreWithRelations([]string{"tool", "method"}, map[string]string{"工具": "tool"}, []string{"uses"})
	tool, err := store.AddEntity(1, "kb", "default", CanonicalEntity{Name: "Same", EntityType: "工具"})
	if err != nil {
		t.Fatal(err)
	}
	method, err := store.AddEntity(1, "kb", "default", CanonicalEntity{Name: "Same", EntityType: "method"})
	if err != nil {
		t.Fatal(err)
	}
	if tool.CanonicalKey == method.CanonicalKey {
		t.Fatalf("entity types must remain isolated: %q", tool.CanonicalKey)
	}
	source := GraphSource{KnowledgeID: "k", ChunkID: "c"}
	if err := store.AddRelationshipEvidence(1, "kb", "default", GraphEdge{Source: tool.CanonicalKey, Target: method.CanonicalKey, RelationType: "unknown", Direction: GraphDirectionOutgoing}, source); err == nil {
		t.Fatal("expected unknown relation to reject the complete write")
	}
}

func TestGraphStoreRebuildIsAtomicAndRollbackKeepsPreviousNamespace(t *testing.T) {
	store := NewGraphStoreWithRelations([]string{"tool"}, nil, []string{"uses"})
	entityA := CanonicalEntity{Name: "A", EntityType: "tool"}
	entityB := CanonicalEntity{Name: "B", EntityType: "tool"}
	a := CanonicalEntityKey(1, "kb", "tool", "A")
	b := CanonicalEntityKey(1, "kb", "tool", "B")
	source := GraphSource{KnowledgeID: "k", ChunkID: "c", ExtractorID: "extractor"}
	if _, err := store.Rebuild(context.Background(), 1, "kb", "v1", []CanonicalEntity{entityA, entityB}, []GraphEdge{{ID: "e", Source: a, Target: b, RelationType: "uses"}}, map[string]GraphSource{"e": source}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Rebuild(context.Background(), 1, "kb", "bad", []CanonicalEntity{entityA}, []GraphEdge{{ID: "bad", Source: a, Target: "missing", RelationType: "uses"}}, map[string]GraphSource{"bad": source}, true); err == nil {
		t.Fatal("expected invalid rebuild to fail")
	}
	if store.ActiveNamespace(1, "kb") != "v1" {
		t.Fatal("failed rebuild must not switch the active namespace")
	}
	if _, err := store.Rebuild(context.Background(), 1, "kb", "v2", []CanonicalEntity{entityA}, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	rolledBack, err := store.RollbackNamespace(1, "kb")
	if err != nil || rolledBack != "v1" || store.ActiveNamespace(1, "kb") != "v1" {
		t.Fatalf("unexpected rollback: namespace=%q err=%v active=%q", rolledBack, err, store.ActiveNamespace(1, "kb"))
	}
}

func TestGraphStoreRebuildRehearsalIsIdempotentAndKeepsSamplePath(t *testing.T) {
	store := NewGraphStoreWithRelations([]string{"tool"}, nil, []string{"uses"})
	entityA := CanonicalEntity{Name: "A", EntityType: "tool"}
	entityB := CanonicalEntity{Name: "B", EntityType: "tool"}
	a := CanonicalEntityKey(1, "kb", "tool", "A")
	b := CanonicalEntityKey(1, "kb", "tool", "B")
	source := GraphSource{KnowledgeID: "source-copy", KnowledgeVersionID: "v1", ChunkID: "chunk-1", ExtractorID: "rehearsal"}
	records := []GraphRebuildRecord{
		{Entity: &entityA},
		{Entity: &entityB},
		{Edge: &GraphEdge{ID: "uses-a-b", Source: a, Target: b, RelationType: "uses", Direction: GraphDirectionOutgoing, Weight: 1}, Source: source},
	}
	first, err := store.RebuildFromRecords(context.Background(), 1, "kb", "copy-v1", records, true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.RebuildFromRecords(context.Background(), 1, "kb", "copy-v2", records, true)
	if err != nil {
		t.Fatal(err)
	}
	if first.EntityCount != 2 || first.RelationshipCount != 1 || second != (GraphRebuildResult{Namespace: "copy-v2", EntityCount: 2, RelationshipCount: 1, Switched: true}) {
		t.Fatalf("rebuild counts are not deterministic: first=%+v second=%+v", first, second)
	}
	sample, err := store.Query(context.Background(), GraphQuery{
		Scope: GraphScope{TenantID: 1, KnowledgeBaseID: "kb", AllowedKnowledgeIDs: []string{"source-copy"}},
		Seeds: []GraphSeed{{Name: "A", EntityType: "tool"}}, RelationTypes: []string{"uses"},
		Direction: GraphDirectionOutgoing, MaxDepth: 1, MaxPaths: 10,
	}, []string{"uses"})
	if err != nil || len(sample.Paths) != 1 || len(sample.Paths[0].Evidence) != 1 {
		t.Fatalf("rebuild sample path is not reproducible: result=%+v err=%v", sample, err)
	}
	rolledBack, err := store.RollbackNamespace(1, "kb")
	if err != nil || rolledBack != "copy-v1" || store.ActiveNamespace(1, "kb") != "copy-v1" {
		t.Fatalf("rebuild rollback rehearsal failed: namespace=%q err=%v active=%q", rolledBack, err, store.ActiveNamespace(1, "kb"))
	}
}

func TestGraphStoreSoftwareTestingFixtureSupportsCrossDocumentThreeHopEvidence(t *testing.T) {
	store := NewGraphStoreWithRelations(
		[]string{"test_object", "quality", "method", "metric"},
		map[string]string{"测评对象": "test_object", "测试方法": "method"},
		[]string{"has_quality", "uses_method", "measured_by"},
	)
	entities := []CanonicalEntity{
		{Name: "登录服务", EntityType: "测评对象"},
		{Name: "可靠性", EntityType: "quality"},
		{Name: "故障注入", EntityType: "测试方法"},
		{Name: "失败率", EntityType: "metric"},
	}
	for _, entity := range entities {
		if _, err := store.AddEntity(1, "software-testing", "default", entity); err != nil {
			t.Fatal(err)
		}
	}
	object := CanonicalEntityKey(1, "software-testing", "test_object", "登录服务")
	quality := CanonicalEntityKey(1, "software-testing", "quality", "可靠性")
	method := CanonicalEntityKey(1, "software-testing", "method", "故障注入")
	metric := CanonicalEntityKey(1, "software-testing", "metric", "失败率")
	sources := []GraphSource{
		{KnowledgeID: "standard-doc", KnowledgeVersionID: "v1", ChunkID: "standard-c1", ExtractorID: "fixture"},
		{KnowledgeID: "internal-doc", KnowledgeVersionID: "v2", ChunkID: "internal-c1", ExtractorID: "fixture"},
		{KnowledgeID: "project-doc", KnowledgeVersionID: "v3", ChunkID: "project-c1", ExtractorID: "fixture"},
	}
	for i, instance := range []DocumentEntityInstance{
		{Name: "登录服务", EntityType: "test_object", KnowledgeID: sources[0].KnowledgeID},
		{Name: "可靠性", EntityType: "quality", KnowledgeID: sources[1].KnowledgeID},
		{Name: "故障注入", EntityType: "method", KnowledgeID: sources[2].KnowledgeID},
		{Name: "失败率", EntityType: "metric", KnowledgeID: sources[2].KnowledgeID},
	} {
		if err := store.AddInstance(1, "software-testing", "default", instance, sources[min(i, len(sources)-1)]); err != nil {
			t.Fatal(err)
		}
	}
	edges := []struct {
		id, source, target, relation string
		proof                        GraphSource
	}{
		{"object-quality", object, quality, "has_quality", sources[0]},
		{"quality-method", quality, method, "uses_method", sources[1]},
		{"method-metric", method, metric, "measured_by", sources[2]},
	}
	for _, edge := range edges {
		if err := store.AddRelationshipEvidence(1, "software-testing", "default", GraphEdge{ID: edge.id, Source: edge.source, Target: edge.target, RelationType: edge.relation, Direction: GraphDirectionOutgoing, Weight: 1}, edge.proof); err != nil {
			t.Fatal(err)
		}
	}
	result, err := store.Query(context.Background(), GraphQuery{
		Scope: GraphScope{TenantID: 1, KnowledgeBaseID: "software-testing", AllowedKnowledgeIDs: []string{"standard-doc", "internal-doc", "project-doc"}},
		Seeds: []GraphSeed{{Name: "登录服务", EntityType: "test_object"}}, RelationTypes: []string{"has_quality", "uses_method", "measured_by"},
		Direction: GraphDirectionOutgoing, MaxDepth: 3, BranchFactor: 2, MaxPaths: 10,
	}, []string{"has_quality", "uses_method", "measured_by"})
	if err != nil {
		t.Fatal(err)
	}
	var threeHop *GraphPath
	for i := range result.Paths {
		if len(result.Paths[i].Edges) == 3 {
			threeHop = &result.Paths[i]
			break
		}
	}
	if threeHop == nil || len(threeHop.Evidence) != 3 {
		t.Fatalf("expected a three-hop evidence path, got %#v", result.Paths)
	}
	for i, evidence := range threeHop.Evidence {
		if evidence.KnowledgeID != sources[i].KnowledgeID || evidence.KnowledgeVersionID != sources[i].KnowledgeVersionID || evidence.ChunkID != sources[i].ChunkID {
			t.Fatalf("edge %d lost cross-document evidence: %#v", i, threeHop.Evidence)
		}
	}
}

func TestGraphStoreRemovesOneDocumentSourceWithoutDeletingSharedGraph(t *testing.T) {
	store := NewGraphStoreWithRelations([]string{"method", "tool"}, map[string]string{"方法": "method"}, []string{"uses"})
	method, err := store.AddEntity(1, "kb", "default", CanonicalEntity{Name: "方法", EntityType: "方法"})
	if err != nil {
		t.Fatal(err)
	}
	tool, err := store.AddEntity(1, "kb", "default", CanonicalEntity{Name: "工具", EntityType: "tool"})
	if err != nil {
		t.Fatal(err)
	}
	first := GraphSource{KnowledgeID: "doc-1", KnowledgeVersionID: "v1", ChunkID: "c1", ExtractorID: "extractor"}
	second := GraphSource{KnowledgeID: "doc-2", KnowledgeVersionID: "v2", ChunkID: "c2", ExtractorID: "extractor"}
	for _, source := range []GraphSource{first, second} {
		if err := store.AddInstance(1, "kb", "default", DocumentEntityInstance{Name: "方法", EntityType: "方法", KnowledgeID: source.KnowledgeID}, source); err != nil {
			t.Fatal(err)
		}
		if err := store.AddRelationshipEvidence(1, "kb", "default", GraphEdge{Source: method.CanonicalKey, Target: tool.CanonicalKey, RelationType: "uses"}, source); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.RemoveSource(1, "kb", "default", first); err != nil {
		t.Fatal(err)
	}
	result, err := store.Query(context.Background(), GraphQuery{Scope: GraphScope{TenantID: 1, KnowledgeBaseID: "kb", AllowedKnowledgeIDs: []string{"doc-2"}}, Seeds: []GraphSeed{{Name: "方法", EntityType: "方法"}}, RelationTypes: []string{"uses"}}, []string{"uses"})
	if err != nil || len(result.Paths) != 1 || len(result.Paths[0].Evidence) != 1 || result.Paths[0].Evidence[0].KnowledgeID != "doc-2" {
		t.Fatalf("shared evidence was not preserved: result=%+v err=%v", result, err)
	}
}

func TestGraphStoreRecordsSameNameTypeConflicts(t *testing.T) {
	store := NewGraphStore([]string{"tool", "method", "metric"}, nil)
	for _, entityType := range []string{"tool", "method", "metric"} {
		if _, err := store.AddEntity(1, "kb", "default", CanonicalEntity{Name: "same", EntityType: entityType}); err != nil {
			t.Fatal(err)
		}
	}
	conflicts, err := store.Conflicts(1, "kb", "default")
	if err != nil || len(conflicts) != 1 {
		t.Fatalf("expected one ambiguity record, conflicts=%#v err=%v", conflicts, err)
	}
	if len(conflicts[0].Types) != 3 || conflicts[0].Types[0] != "method" || conflicts[0].Types[2] != "tool" {
		t.Fatalf("expected all conflicting types, got %#v", conflicts[0])
	}
}

func TestGraphStoreRebuildMergesRepeatedRelationshipEvidence(t *testing.T) {
	store := NewGraphStoreWithRelations([]string{"tool"}, nil, []string{"uses"})
	a := CanonicalEntityKey(1, "kb", "tool", "a")
	b := CanonicalEntityKey(1, "kb", "tool", "b")
	edge := GraphEdge{ID: "shared", Source: a, Target: b, RelationType: "uses"}
	records := []GraphRebuildRecord{
		{Entity: &CanonicalEntity{Name: "a", EntityType: "tool"}},
		{Entity: &CanonicalEntity{Name: "b", EntityType: "tool"}},
		{Edge: &edge, Source: GraphSource{KnowledgeID: "doc-1", ChunkID: "chunk-1", ExtractorID: "extractor"}},
		{Edge: &edge, Source: GraphSource{KnowledgeID: "doc-2", ChunkID: "chunk-2", ExtractorID: "extractor"}},
	}
	if _, err := store.RebuildFromRecords(context.Background(), 1, "kb", "rebuild", records, true); err != nil {
		t.Fatal(err)
	}
	result, err := store.Query(context.Background(), GraphQuery{Scope: GraphScope{TenantID: 1, KnowledgeBaseID: "kb", AllowedKnowledgeIDs: []string{"doc-1", "doc-2"}}, Seeds: []GraphSeed{{Name: "a", EntityType: "tool"}}, RelationTypes: []string{"uses"}}, []string{"uses"})
	if err != nil || len(result.Paths) != 1 || len(result.Paths[0].Evidence) != 2 {
		t.Fatalf("rebuild lost repeated edge evidence: result=%#v err=%v", result, err)
	}
}
