package neo4j

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
)

const (
	canonicalEntityLabel    = "CanonicalEntity"
	canonicalInstanceLabel  = "DocumentEntityInstance"
	canonicalRelationLabel  = "CanonicalRelation"
	canonicalEvidenceLabel  = "GraphEvidence"
	canonicalNamespaceLabel = "GraphNamespace"
)

func (n *Neo4jRepository) EnsureCanonicalSchema(ctx context.Context) error {
	if n.driver == nil {
		return fmt.Errorf("neo4j driver is unavailable")
	}
	session := n.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		queries := []string{
			"CREATE CONSTRAINT canonical_entity_identity IF NOT EXISTS FOR (n:CanonicalEntity) REQUIRE (n.tenant_id, n.knowledge_base_id, n.namespace, n.entity_type, n.normalized_name) IS UNIQUE",
			"CREATE CONSTRAINT canonical_instance_identity IF NOT EXISTS FOR (n:DocumentEntityInstance) REQUIRE n.instance_key IS UNIQUE",
			"CREATE CONSTRAINT canonical_relation_identity IF NOT EXISTS FOR (n:CanonicalRelation) REQUIRE n.relation_key IS UNIQUE",
			"CREATE CONSTRAINT canonical_evidence_identity IF NOT EXISTS FOR (n:GraphEvidence) REQUIRE n.evidence_key IS UNIQUE",
			"CREATE INDEX canonical_entity_key IF NOT EXISTS FOR (n:CanonicalEntity) ON (n.canonical_key)",
			"CREATE INDEX canonical_instance_knowledge IF NOT EXISTS FOR (n:DocumentEntityInstance) ON (n.knowledge_id)",
			"CREATE INDEX canonical_namespace_lookup IF NOT EXISTS FOR (n:GraphNamespace) ON (n.tenant_id, n.knowledge_base_id)",
			"CREATE INDEX canonical_conflict_lookup IF NOT EXISTS FOR (n:GraphConflict) ON (n.tenant_id, n.knowledge_base_id, n.namespace)",
		}
		for _, query := range queries {
			if _, err := tx.Run(ctx, query, nil); err != nil {
				return nil, fmt.Errorf("create canonical graph schema: %w", err)
			}
		}
		return nil, nil
	})
	return err
}

func (n *Neo4jRepository) UpsertCanonicalRecords(ctx context.Context, tenantID uint64, knowledgeBaseID, namespace string, records []types.GraphRebuildRecord) error {
	if tenantID == 0 || strings.TrimSpace(knowledgeBaseID) == "" || strings.TrimSpace(namespace) == "" {
		return fmt.Errorf("tenant, knowledge base and namespace are required")
	}
	if n.driver == nil {
		return fmt.Errorf("neo4j driver is unavailable")
	}
	if err := n.EnsureCanonicalSchema(ctx); err != nil {
		return err
	}
	session := n.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		return nil, upsertCanonicalRecordsInTx(ctx, tx, tenantID, knowledgeBaseID, namespace, records)
	})
	return err
}

func upsertCanonicalRecordsInTx(ctx context.Context, tx neo4j.ManagedTransaction, tenantID uint64, knowledgeBaseID, namespace string, records []types.GraphRebuildRecord) error {
	for _, record := range records {
		if record.Entity == nil {
			continue
		}
		if err := upsertCanonicalRecord(ctx, tx, tenantID, knowledgeBaseID, namespace, types.GraphRebuildRecord{Entity: record.Entity}); err != nil {
			return err
		}
	}
	for _, record := range records {
		if err := upsertCanonicalRecord(ctx, tx, tenantID, knowledgeBaseID, namespace, record); err != nil {
			return err
		}
	}
	return nil
}

func upsertCanonicalRecord(ctx context.Context, tx neo4j.ManagedTransaction, tenantID uint64, knowledgeBaseID, namespace string, record types.GraphRebuildRecord) error {
	if record.Entity != nil {
		entity := *record.Entity
		key := types.CanonicalEntityKey(tenantID, knowledgeBaseID, entity.EntityType, entity.Name)
		if entity.CanonicalKey != "" && entity.CanonicalKey != key {
			return fmt.Errorf("canonical entity key does not match normalized identity")
		}
		aliases := normalizeGraphAliases(entity.Aliases)
		if _, err := tx.Run(ctx, `MERGE (entity:CanonicalEntity {canonical_key: $canonical_key, namespace: $namespace})
SET entity.tenant_id = $tenant_id, entity.knowledge_base_id = $knowledge_base_id,
    entity.name = $name, entity.normalized_name = $normalized_name,
    entity.entity_type = $entity_type, entity.aliases = $aliases, entity.normalized_aliases = $normalized_aliases`, map[string]interface{}{
			"canonical_key": key, "tenant_id": tenantID, "knowledge_base_id": knowledgeBaseID,
			"namespace": namespace, "name": strings.TrimSpace(entity.Name), "normalized_name": types.NormalizeEntityName(entity.Name),
			"entity_type": types.NormalizeEntityName(entity.EntityType), "aliases": entity.Aliases, "normalized_aliases": aliases,
		}); err != nil {
			return fmt.Errorf("upsert canonical entity: %w", err)
		}
		if err := recordCanonicalAliasConflicts(ctx, tx, tenantID, knowledgeBaseID, namespace, key, types.NormalizeEntityName(entity.EntityType), aliases); err != nil {
			return err
		}
	}
	if record.Instance != nil {
		instance := *record.Instance
		if record.Source.KnowledgeID == "" || record.Source.ChunkID == "" || instance.KnowledgeID == "" {
			return fmt.Errorf("document entity instance requires authoritative knowledge and chunk source")
		}
		if record.Source.KnowledgeID != instance.KnowledgeID {
			return fmt.Errorf("instance source knowledge does not match instance knowledge")
		}
		canonicalKey := types.CanonicalEntityKey(tenantID, knowledgeBaseID, instance.EntityType, instance.Name)
		instanceKey := strings.Join([]string{namespace, canonicalKey, instance.KnowledgeID, record.Source.ChunkID}, "|")
		if _, err := tx.Run(ctx, `MATCH (entity:CanonicalEntity {canonical_key: $canonical_key, namespace: $namespace})
MERGE (instance:DocumentEntityInstance {instance_key: $instance_key})
SET instance.tenant_id = $tenant_id, instance.knowledge_base_id = $knowledge_base_id,
    instance.namespace = $namespace, instance.knowledge_id = $knowledge_id,
    instance.chunk_id = $chunk_id, instance.name = $name, instance.entity_type = $entity_type
MERGE (instance)-[:INSTANCE_OF]->(entity)`, map[string]interface{}{
			"canonical_key": canonicalKey, "instance_key": instanceKey, "tenant_id": tenantID,
			"knowledge_base_id": knowledgeBaseID, "namespace": namespace, "knowledge_id": instance.KnowledgeID,
			"chunk_id": record.Source.ChunkID, "name": strings.TrimSpace(instance.Name), "entity_type": types.NormalizeEntityName(instance.EntityType),
		}); err != nil {
			return fmt.Errorf("upsert document entity instance: %w", err)
		}
	}
	if record.Edge != nil {
		if record.Source.KnowledgeID == "" || record.Source.ChunkID == "" {
			return fmt.Errorf("relationship requires authoritative knowledge and chunk source")
		}
		edge := *record.Edge
		if edge.Source == "" || edge.Target == "" || edge.RelationType == "" {
			return fmt.Errorf("relationship source, target and type are required")
		}
		direction := edge.Direction
		if direction == "" {
			direction = types.GraphDirectionOutgoing
		}
		relationKey := strings.Join([]string{namespace, fmt.Sprint(tenantID), knowledgeBaseID, edge.Source, strings.ToLower(strings.TrimSpace(edge.RelationType)), string(direction), edge.Target}, "|")
		evidenceKey := strings.Join([]string{relationKey, graphSourceEvidenceKey(record.Source)}, "|")
		if _, err := tx.Run(ctx, `MERGE (relation:CanonicalRelation {relation_key: $relation_key})
SET relation.tenant_id = $tenant_id, relation.knowledge_base_id = $knowledge_base_id,
    relation.namespace = $namespace, relation.source = $source, relation.target = $target,
    relation.relation_type = $relation_type, relation.direction = $direction, relation.weight = $weight
WITH relation
MATCH (source:CanonicalEntity {canonical_key: $source, namespace: $namespace}), (target:CanonicalEntity {canonical_key: $target, namespace: $namespace})
MERGE (relation)-[:CONNECTS_FROM]->(source)
MERGE (relation)-[:CONNECTS_TO]->(target)
MERGE (evidence:GraphEvidence {evidence_key: $evidence_key})
SET evidence.tenant_id = $tenant_id, evidence.knowledge_base_id = $knowledge_base_id,
    evidence.knowledge_id = $knowledge_id, evidence.chunk_id = $chunk_id,
    evidence.knowledge_version_id = $knowledge_version_id, evidence.extractor_id = $extractor_id,
    evidence.namespace = $namespace, evidence.weight = $weight
MERGE (relation)-[:EVIDENCED_BY]->(evidence)`, map[string]interface{}{
			"relation_key": relationKey, "tenant_id": tenantID, "knowledge_base_id": knowledgeBaseID,
			"namespace": namespace, "source": edge.Source, "target": edge.Target,
			"relation_type": strings.ToLower(strings.TrimSpace(edge.RelationType)), "direction": string(direction),
			"weight": edge.Weight, "evidence_key": evidenceKey, "knowledge_id": record.Source.KnowledgeID,
			"chunk_id": record.Source.ChunkID, "knowledge_version_id": record.Source.KnowledgeVersionID, "extractor_id": record.Source.ExtractorID,
		}); err != nil {
			return fmt.Errorf("upsert canonical relationship: %w", err)
		}
	}
	return nil
}

func graphSourceEvidenceKey(s types.GraphSource) string {
	return strings.Join([]string{s.KnowledgeID, s.KnowledgeVersionID, s.ChunkID, s.ExtractorID}, "|")
}

func normalizeGraphAliases(aliases []string) []string {
	seen := make(map[string]struct{}, len(aliases))
	result := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		normalized := types.NormalizeEntityName(alias)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func recordCanonicalAliasConflicts(ctx context.Context, tx neo4j.ManagedTransaction, tenantID uint64, knowledgeBaseID, namespace, canonicalKey, entityType string, aliases []string) error {
	if len(aliases) == 0 {
		return nil
	}
	_, err := tx.Run(ctx, `UNWIND $aliases AS alias
MATCH (other:CanonicalEntity {tenant_id: $tenant_id, knowledge_base_id: $knowledge_base_id,
  namespace: $namespace, entity_type: $entity_type})
WHERE other.canonical_key <> $canonical_key AND alias IN coalesce(other.normalized_aliases, [])
MERGE (conflict:GraphConflict {conflict_key: $conflict_prefix + alias + "|" + other.canonical_key})
SET conflict.tenant_id = $tenant_id, conflict.knowledge_base_id = $knowledge_base_id,
    conflict.namespace = $namespace, conflict.alias = alias,
    conflict.entity_type = $entity_type, conflict.left_key = $canonical_key,
    conflict.right_key = other.canonical_key`, map[string]interface{}{
		"aliases": aliases, "tenant_id": tenantID, "knowledge_base_id": knowledgeBaseID,
		"namespace": namespace, "entity_type": entityType, "canonical_key": canonicalKey,
		"conflict_prefix": strings.Join([]string{namespace, fmt.Sprint(tenantID), knowledgeBaseID, entityType, ""}, "|"),
	})
	if err != nil {
		return fmt.Errorf("record canonical alias conflict: %w", err)
	}
	return nil
}

func (n *Neo4jRepository) RemoveCanonicalSource(ctx context.Context, tenantID uint64, knowledgeBaseID, namespace string, source types.GraphSource) error {
	if n.driver == nil {
		return fmt.Errorf("neo4j driver is unavailable")
	}
	session := n.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		return nil, removeCanonicalSourceInTx(ctx, tx, tenantID, knowledgeBaseID, namespace, source)
	})
	return err
}

// ReplaceCanonicalSourceRecords atomically removes all graph facts contributed
// by one chunk and writes the new extraction result.
func (n *Neo4jRepository) ReplaceCanonicalSourceRecords(ctx context.Context, tenantID uint64, knowledgeBaseID, namespace string, source types.GraphSource, records []types.GraphRebuildRecord) error {
	if tenantID == 0 || strings.TrimSpace(knowledgeBaseID) == "" || strings.TrimSpace(namespace) == "" {
		return fmt.Errorf("tenant, knowledge base and namespace are required")
	}
	if source.KnowledgeID == "" || source.ChunkID == "" {
		return fmt.Errorf("knowledge and chunk source are required")
	}
	if n.driver == nil {
		return fmt.Errorf("neo4j driver is unavailable")
	}
	if err := n.EnsureCanonicalSchema(ctx); err != nil {
		return err
	}
	session := n.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		if err := removeCanonicalSourceInTx(ctx, tx, tenantID, knowledgeBaseID, namespace, source); err != nil {
			return nil, err
		}
		return nil, upsertCanonicalRecordsInTx(ctx, tx, tenantID, knowledgeBaseID, namespace, records)
	})
	return err
}

func removeCanonicalSourceInTx(ctx context.Context, tx neo4j.ManagedTransaction, tenantID uint64, knowledgeBaseID, namespace string, source types.GraphSource) error {
	params := map[string]interface{}{"tenant_id": tenantID, "knowledge_base_id": knowledgeBaseID, "namespace": namespace, "knowledge_id": source.KnowledgeID, "knowledge_version_id": source.KnowledgeVersionID, "chunk_id": source.ChunkID, "extractor_id": source.ExtractorID}
	queries := []string{
		`MATCH (e:GraphEvidence {tenant_id: $tenant_id, knowledge_base_id: $knowledge_base_id, namespace: $namespace, knowledge_id: $knowledge_id, chunk_id: $chunk_id})
WHERE ($knowledge_version_id = '' OR coalesce(e.knowledge_version_id, '') = $knowledge_version_id)
  AND ($extractor_id = '' OR e.extractor_id = $extractor_id)
DETACH DELETE e`,
		`MATCH (i:DocumentEntityInstance {tenant_id: $tenant_id, knowledge_base_id: $knowledge_base_id, namespace: $namespace, knowledge_id: $knowledge_id, chunk_id: $chunk_id})
DETACH DELETE i`,
		`MATCH (r:CanonicalRelation {tenant_id: $tenant_id, knowledge_base_id: $knowledge_base_id, namespace: $namespace})
WHERE NOT (r)-[:EVIDENCED_BY]->()
DETACH DELETE r`,
		`MATCH (e:CanonicalEntity {tenant_id: $tenant_id, knowledge_base_id: $knowledge_base_id, namespace: $namespace})
WHERE NOT (e)<-[:INSTANCE_OF]-()
  AND NOT (e)<-[:CONNECTS_FROM]-()
  AND NOT (e)<-[:CONNECTS_TO]-()
DETACH DELETE e`,
	}
	for _, query := range queries {
		if _, err := tx.Run(ctx, query, params); err != nil {
			return fmt.Errorf("remove canonical source: %w", err)
		}
	}
	return nil
}

func (n *Neo4jRepository) DeleteCanonicalKnowledgeBase(ctx context.Context, tenantID uint64, knowledgeBaseID string) error {
	if n.driver == nil {
		return fmt.Errorf("neo4j driver is unavailable")
	}
	session := n.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		_, runErr := tx.Run(ctx, `MATCH (n)
WHERE n.tenant_id = $tenant_id AND n.knowledge_base_id = $knowledge_base_id
  AND (n:CanonicalEntity OR n:DocumentEntityInstance OR n:CanonicalRelation
    OR n:GraphEvidence OR n:GraphNamespace OR n:GraphConflict)
DETACH DELETE n`, map[string]interface{}{
			"tenant_id": tenantID, "knowledge_base_id": knowledgeBaseID,
		})
		return nil, runErr
	})
	if err != nil {
		return fmt.Errorf("delete canonical knowledge base: %w", err)
	}
	return nil
}

func (n *Neo4jRepository) RebuildCanonicalGraph(ctx context.Context, tenantID uint64, knowledgeBaseID, namespace string, records []types.GraphRebuildRecord, switchActive bool) (types.GraphRebuildResult, error) {
	if err := n.UpsertCanonicalRecords(ctx, tenantID, knowledgeBaseID, namespace, records); err != nil {
		return types.GraphRebuildResult{}, err
	}
	if switchActive {
		if err := n.SwitchCanonicalNamespace(ctx, tenantID, knowledgeBaseID, namespace); err != nil {
			return types.GraphRebuildResult{}, err
		}
	}
	counts, err := n.canonicalCounts(ctx, tenantID, knowledgeBaseID, namespace)
	if err != nil {
		return types.GraphRebuildResult{}, err
	}
	return types.GraphRebuildResult{Namespace: namespace, EntityCount: counts[0], InstanceCount: counts[1], RelationshipCount: counts[2], Switched: switchActive}, nil
}

func (n *Neo4jRepository) canonicalCounts(ctx context.Context, tenantID uint64, knowledgeBaseID, namespace string) ([3]int, error) {
	var counts [3]int
	session := n.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)
	_, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		queries := []string{
			`MATCH (n:CanonicalEntity {tenant_id: $tenant_id, knowledge_base_id: $knowledge_base_id, namespace: $namespace}) RETURN count(n) AS count`,
			`MATCH (n:DocumentEntityInstance {tenant_id: $tenant_id, knowledge_base_id: $knowledge_base_id, namespace: $namespace}) RETURN count(n) AS count`,
			`MATCH (n:CanonicalRelation {tenant_id: $tenant_id, knowledge_base_id: $knowledge_base_id, namespace: $namespace}) RETURN count(n) AS count`,
		}
		for index, query := range queries {
			result, err := tx.Run(ctx, query, map[string]interface{}{"tenant_id": tenantID, "knowledge_base_id": knowledgeBaseID, "namespace": namespace})
			if err != nil {
				return nil, err
			}
			record, err := result.Single(ctx)
			if err != nil {
				return nil, err
			}
			value, ok := record.Get("count")
			if !ok {
				return nil, fmt.Errorf("canonical count is missing")
			}
			if number, ok := value.(int64); ok {
				counts[index] = int(number)
			}
		}
		return nil, nil
	})
	return counts, err
}

func (n *Neo4jRepository) SwitchCanonicalNamespace(ctx context.Context, tenantID uint64, knowledgeBaseID, namespace string) error {
	return n.setCanonicalNamespace(ctx, tenantID, knowledgeBaseID, namespace, true)
}

func (n *Neo4jRepository) RollbackCanonicalNamespace(ctx context.Context, tenantID uint64, knowledgeBaseID string) (string, error) {
	if n.driver == nil {
		return "", fmt.Errorf("neo4j driver is unavailable")
	}
	session := n.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)
	result, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `MATCH (state:GraphNamespace {tenant_id: $tenant_id, knowledge_base_id: $knowledge_base_id})
WITH state, state.previous_namespaces AS history
WHERE history IS NOT NULL AND size(history) > 0
SET state.active_namespace = history[size(history) - 1], state.previous_namespaces = history[0..size(history)-1]
RETURN state.active_namespace AS namespace`
		cursor, err := tx.Run(ctx, query, map[string]interface{}{"tenant_id": tenantID, "knowledge_base_id": knowledgeBaseID})
		if err != nil {
			return nil, err
		}
		record, err := cursor.Single(ctx)
		if err != nil {
			return nil, fmt.Errorf("canonical namespace rollback history is empty: %w", err)
		}
		value, _ := record.Get("namespace")
		return fmt.Sprint(value), nil
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

func (n *Neo4jRepository) setCanonicalNamespace(ctx context.Context, tenantID uint64, knowledgeBaseID, namespace string, rememberPrevious bool) error {
	if n.driver == nil {
		return fmt.Errorf("neo4j driver is unavailable")
	}
	session := n.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `MERGE (state:GraphNamespace {tenant_id: $tenant_id, knowledge_base_id: $knowledge_base_id})
WITH state, state.active_namespace AS previous
SET state.active_namespace = $namespace,
    state.previous_namespaces = CASE WHEN $remember_previous AND previous IS NOT NULL AND previous <> $namespace
      THEN coalesce(state.previous_namespaces, []) + previous ELSE coalesce(state.previous_namespaces, []) END
RETURN state`
		_, err := tx.Run(ctx, query, map[string]interface{}{"tenant_id": tenantID, "knowledge_base_id": knowledgeBaseID, "namespace": namespace, "remember_previous": rememberPrevious})
		return nil, err
	})
	return err
}

func (n *Neo4jRepository) activeCanonicalNamespace(ctx context.Context, tenantID uint64, knowledgeBaseID string) (string, error) {
	if n.driver == nil {
		return "", fmt.Errorf("neo4j driver is unavailable")
	}
	session := n.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)
	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		cursor, err := tx.Run(ctx, `MATCH (state:GraphNamespace {tenant_id: $tenant_id, knowledge_base_id: $knowledge_base_id})
RETURN state.active_namespace AS namespace`, map[string]interface{}{"tenant_id": tenantID, "knowledge_base_id": knowledgeBaseID})
		if err != nil {
			return nil, err
		}
		if cursor.Next(ctx) {
			value, _ := cursor.Record().Get("namespace")
			if namespace := strings.TrimSpace(fmt.Sprint(value)); namespace != "" && namespace != "<nil>" {
				return namespace, nil
			}
		}
		if err := cursor.Err(); err != nil {
			return nil, err
		}
		return "default", nil
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

func (n *Neo4jRepository) searchCanonicalPaths(ctx context.Context, query types.GraphQuery) (types.GraphSearchResult, bool, error) {
	query.RelationTypes = normalizedRelationTypes(query.RelationTypes)
	if err := query.Scope.Validate(); err != nil {
		return types.GraphSearchResult{}, false, err
	}
	if err := query.Validate(query.RelationTypes); err != nil {
		return types.GraphSearchResult{}, false, err
	}
	if n.driver == nil {
		return types.GraphSearchResult{}, false, nil
	}
	if query.MaxDepth == 0 {
		query.MaxDepth = 2
	}
	if query.BranchFactor == 0 {
		query.BranchFactor = 10
	}
	if query.MaxExpandedNodes == 0 {
		query.MaxExpandedNodes = 1000
	}
	if query.MaxPaths == 0 {
		query.MaxPaths = 100
	}
	if query.Timeout == 0 {
		query.Timeout = 2 * time.Second
	}
	searchCtx, cancel := context.WithTimeout(ctx, query.Timeout)
	defer cancel()
	namespaces, err := n.canonicalSearchNamespaces(searchCtx, query.Scope)
	if err != nil {
		return types.GraphSearchResult{}, false, err
	}
	seeds := make([]map[string]interface{}, 0, len(query.Seeds))
	for _, seed := range query.Seeds {
		seeds = append(seeds, map[string]interface{}{
			"canonical_key":   seed.CanonicalKey,
			"normalized_name": types.NormalizeEntityName(seed.Name),
			"entity_type":     types.NormalizeEntityName(seed.EntityType),
		})
	}
	nodes, err := n.loadCanonicalSeeds(searchCtx, query.Scope, namespaces, seeds)
	if err != nil {
		return types.GraphSearchResult{}, false, err
	}
	if len(nodes) == 0 {
		return types.GraphSearchResult{}, false, nil
	}
	nodeMap := make(map[string]types.CanonicalEntity, len(nodes))
	visited := make(map[string]struct{}, len(nodes))
	frontier := make([]string, 0, len(nodes))
	for _, node := range nodes {
		nodeMap[node.CanonicalKey] = node
		visited[node.CanonicalKey] = struct{}{}
		frontier = append(frontier, node.CanonicalKey)
	}
	sort.Strings(frontier)
	edges := make(map[string]types.GraphEdge)
	truncated := false
	truncationReason := ""
	for depth := 0; depth < query.MaxDepth && len(frontier) > 0; depth++ {
		frontierResult, err := n.loadCanonicalFrontier(searchCtx, query.Scope, namespaces, frontier, query.RelationTypes)
		if err != nil {
			return types.GraphSearchResult{}, false, err
		}
		for _, node := range frontierResult.Nodes {
			nodeMap[node.CanonicalKey] = node
		}
		rows := frontierResult.Edges
		nextFrontier := make([]string, 0)
		for _, current := range frontier {
			candidates := make([]types.GraphEdge, 0)
			for _, edge := range rows {
				if _, ok := graphEdgeTargetForQuery(edge, current, query.Direction); ok {
					candidates = append(candidates, edge)
				}
			}
			sort.Slice(candidates, func(i, j int) bool {
				if candidates[i].Weight == candidates[j].Weight {
					return candidates[i].ID < candidates[j].ID
				}
				return candidates[i].Weight > candidates[j].Weight
			})
			if len(candidates) > query.BranchFactor {
				candidates = candidates[:query.BranchFactor]
				truncated, truncationReason = true, "branch_factor"
			}
			for _, edge := range candidates {
				edges[edge.ID] = mergeCanonicalEdge(edges[edge.ID], edge)
				target, _ := graphEdgeTargetForQuery(edge, current, query.Direction)
				if _, exists := visited[target]; exists {
					continue
				}
				if len(visited) >= query.MaxExpandedNodes {
					truncated, truncationReason = true, "expanded_nodes"
					continue
				}
				visited[target] = struct{}{}
				nextFrontier = append(nextFrontier, target)
			}
		}
		sort.Strings(nextFrontier)
		frontier = uniqueStrings(nextFrontier)
	}
	result, err := types.TraverseGraph(searchCtx, query, mapCanonicalEntities(nodeMap), mapCanonicalEdges(edges), query.RelationTypes)
	if err != nil {
		return types.GraphSearchResult{}, false, err
	}
	if truncated {
		result.Truncated = true
		result.TruncationReason = truncationReason
	}
	return result, true, nil
}

func (n *Neo4jRepository) canonicalSearchNamespaces(ctx context.Context, scope types.GraphScope) ([]string, error) {
	active, err := n.activeCanonicalNamespace(ctx, scope.TenantID, scope.KnowledgeBaseID)
	if err != nil {
		return nil, err
	}
	// Keep the unversioned namespace searchable while legacy documents are
	// progressively rebuilt alongside governed version namespaces.
	namespaces := []string{"default", active}
	for _, versionID := range scope.CurrentKnowledgeVersions {
		if namespace := types.GraphNamespaceForVersion(versionID, true); namespace != "default" {
			namespaces = append(namespaces, namespace)
		}
	}
	sort.Strings(namespaces)
	return uniqueStrings(namespaces), nil
}

func (n *Neo4jRepository) loadCanonicalSeeds(ctx context.Context, scope types.GraphScope, namespaces []string, seeds []map[string]interface{}) ([]types.CanonicalEntity, error) {
	session := n.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)
	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		cursor, err := tx.Run(ctx, `UNWIND $seeds AS seed
MATCH (node:CanonicalEntity {tenant_id: $tenant_id, knowledge_base_id: $knowledge_base_id})
WHERE node.namespace IN $namespaces AND (
	(seed.canonical_key <> '' AND node.canonical_key = seed.canonical_key)
   OR (seed.canonical_key = '' AND seed.normalized_name <> '' AND
      (node.normalized_name = seed.normalized_name OR seed.normalized_name IN coalesce(node.normalized_aliases, [])) AND
      (seed.entity_type = '' OR node.entity_type = seed.entity_type)))
RETURN node`, map[string]interface{}{
			"seeds": seeds, "tenant_id": scope.TenantID, "knowledge_base_id": scope.KnowledgeBaseID, "namespaces": namespaces,
		})
		if err != nil {
			return nil, err
		}
		entities := make(map[string]types.CanonicalEntity)
		for cursor.Next(ctx) {
			value, _ := cursor.Record().Get("node")
			node, ok := neo4jNode(value)
			if !ok {
				continue
			}
			entity, ok := canonicalEntityFromNode(node)
			if ok {
				entities[entity.CanonicalKey] = entity
			}
		}
		if err := cursor.Err(); err != nil {
			return nil, err
		}
		result := make([]types.CanonicalEntity, 0, len(entities))
		for _, entity := range entities {
			result = append(result, entity)
		}
		sort.Slice(result, func(i, j int) bool { return result[i].CanonicalKey < result[j].CanonicalKey })
		return result, nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]types.CanonicalEntity), nil
}

type canonicalFrontierResult struct {
	Nodes []types.CanonicalEntity
	Edges []types.GraphEdge
}

func (n *Neo4jRepository) loadCanonicalFrontier(ctx context.Context, scope types.GraphScope, namespaces []string, frontier, relationTypes []string) (canonicalFrontierResult, error) {
	session := n.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)
	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		cursor, err := tx.Run(ctx, `MATCH (source:CanonicalEntity {tenant_id: $tenant_id, knowledge_base_id: $knowledge_base_id})
<-[:CONNECTS_FROM]-(relation:CanonicalRelation {tenant_id: $tenant_id, knowledge_base_id: $knowledge_base_id})
-[:CONNECTS_TO]->(target:CanonicalEntity {tenant_id: $tenant_id, knowledge_base_id: $knowledge_base_id})
MATCH (relation)-[:EVIDENCED_BY]->(evidence:GraphEvidence {tenant_id: $tenant_id, knowledge_base_id: $knowledge_base_id})
WHERE relation.namespace IN $namespaces
  AND source.namespace = relation.namespace
  AND target.namespace = relation.namespace
  AND evidence.namespace = relation.namespace
  AND (source.canonical_key IN $frontier OR target.canonical_key IN $frontier)
  AND ($relation_types = [] OR relation.relation_type IN $relation_types)
  AND ($allowed_knowledge_ids = [] OR evidence.knowledge_id IN $allowed_knowledge_ids)
  AND ($current_version_ids = [] OR coalesce(evidence.knowledge_version_id, '') = '' OR evidence.knowledge_version_id IN $current_version_ids)
RETURN source, target, relation, collect(evidence) AS evidences`, map[string]interface{}{
			"tenant_id": scope.TenantID, "knowledge_base_id": scope.KnowledgeBaseID, "namespaces": namespaces,
			"frontier": frontier, "relation_types": normalizedRelationTypes(relationTypes), "allowed_knowledge_ids": scope.AllowedKnowledgeIDs,
			"current_version_ids": currentVersionIDs(scope.CurrentKnowledgeVersions),
		})
		if err != nil {
			return canonicalFrontierResult{}, err
		}
		edges := make(map[string]types.GraphEdge)
		nodes := make(map[string]types.CanonicalEntity)
		for cursor.Next(ctx) {
			record := cursor.Record()
			sourceValue, _ := record.Get("source")
			targetValue, _ := record.Get("target")
			relationValue, _ := record.Get("relation")
			source, sourceOK := neo4jNode(sourceValue)
			target, targetOK := neo4jNode(targetValue)
			relation, relationOK := neo4jNode(relationValue)
			if !sourceOK || !targetOK || !relationOK {
				continue
			}
			if sourceEntity, ok := canonicalEntityFromNode(source); ok {
				nodes[sourceEntity.CanonicalKey] = sourceEntity
			}
			if targetEntity, ok := canonicalEntityFromNode(target); ok {
				nodes[targetEntity.CanonicalKey] = targetEntity
			}
			edge := canonicalEdgeFromRelationNode(relation, source, target)
			if edge.ID == "" {
				continue
			}
			relationWeight := edge.Weight
			edge.Weight = 0
			evidenceValue, _ := record.Get("evidences")
			for _, item := range neo4jList(evidenceValue) {
				evidenceNode, ok := neo4jNode(item)
				if ok {
					evidence := graphEvidenceFromNode(evidenceNode)
					edge.Evidence = appendUniqueGraphEvidence(edge.Evidence, evidence)
					if evidence.Weight > edge.Weight {
						edge.Weight = evidence.Weight
					}
				}
			}
			// Evidence written before per-source weights were introduced still
			// relies on the relation-level value.
			if edge.Weight == 0 {
				edge.Weight = relationWeight
			}
			if len(edge.Evidence) > 0 {
				edges[edge.ID] = mergeCanonicalEdge(edges[edge.ID], edge)
			}
		}
		if err := cursor.Err(); err != nil {
			return canonicalFrontierResult{}, err
		}
		result := make([]types.GraphEdge, 0, len(edges))
		for _, edge := range edges {
			result = append(result, edge)
		}
		return canonicalFrontierResult{Nodes: mapCanonicalEntities(nodes), Edges: result}, nil
	})
	if err != nil {
		return canonicalFrontierResult{}, err
	}
	return result.(canonicalFrontierResult), nil
}

func normalizedRelationTypes(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func currentVersionIDs(versions map[string]string) []string {
	result := make([]string, 0, len(versions))
	for _, versionID := range versions {
		if versionID = strings.TrimSpace(versionID); versionID != "" {
			result = append(result, versionID)
		}
	}
	sort.Strings(result)
	return uniqueStrings(result)
}

func neo4jNode(value interface{}) (neo4j.Node, bool) {
	switch node := value.(type) {
	case neo4j.Node:
		return node, true
	case *neo4j.Node:
		if node != nil {
			return *node, true
		}
	}
	return neo4j.Node{}, false
}

func neo4jRelationship(value interface{}) (neo4j.Relationship, bool) {
	switch relationship := value.(type) {
	case neo4j.Relationship:
		return relationship, true
	case *neo4j.Relationship:
		if relationship != nil {
			return *relationship, true
		}
	}
	return neo4j.Relationship{}, false
}

func neo4jList(value interface{}) []interface{} {
	switch values := value.(type) {
	case []interface{}:
		return values
	default:
		return nil
	}
}

func stringGraphProperty(props map[string]interface{}, key string) string {
	value, ok := props[key]
	if !ok || value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func floatGraphProperty(props map[string]interface{}, key string) float64 {
	value, ok := props[key]
	if !ok || value == nil {
		return 0
	}
	switch number := value.(type) {
	case float64:
		return number
	case float32:
		return float64(number)
	case int:
		return float64(number)
	case int64:
		return float64(number)
	default:
		parsed, _ := strconv.ParseFloat(fmt.Sprint(value), 64)
		return parsed
	}
}

func stringListGraphProperty(props map[string]interface{}, key string) []string {
	result := make([]string, 0)
	for _, value := range neo4jList(props[key]) {
		if text := strings.TrimSpace(fmt.Sprint(value)); text != "" && text != "<nil>" {
			result = append(result, text)
		}
	}
	return result
}

func canonicalEntityFromNode(node neo4j.Node) (types.CanonicalEntity, bool) {
	key := stringGraphProperty(node.Props, "canonical_key")
	if key == "" {
		return types.CanonicalEntity{}, false
	}
	return types.CanonicalEntity{
		CanonicalKey:    key,
		Name:            stringGraphProperty(node.Props, "name"),
		EntityType:      stringGraphProperty(node.Props, "entity_type"),
		TenantID:        uint64(floatGraphProperty(node.Props, "tenant_id")),
		KnowledgeBaseID: stringGraphProperty(node.Props, "knowledge_base_id"),
		Aliases:         stringListGraphProperty(node.Props, "aliases"),
	}, true
}

func canonicalEdgeFromRelationship(relationship neo4j.Relationship, source, target neo4j.Node) types.GraphEdge {
	return canonicalEdgeFromProperties(relationship.Props, relationship.Type, source, target)
}

func canonicalEdgeFromRelationNode(relation neo4j.Node, source, target neo4j.Node) types.GraphEdge {
	return canonicalEdgeFromProperties(relation.Props, "CanonicalRelation", source, target)
}

func canonicalEdgeFromProperties(props map[string]interface{}, fallbackType string, source, target neo4j.Node) types.GraphEdge {
	identity := stringGraphProperty(props, "relation_key")
	relationType := stringGraphProperty(props, "relation_type")
	if relationType == "" {
		relationType = fallbackType
	}
	if identity == "" {
		identity = strings.Join([]string{stringGraphProperty(source.Props, "canonical_key"), relationType, stringGraphProperty(target.Props, "canonical_key")}, "|")
	}
	direction := types.GraphDirection(stringGraphProperty(props, "direction"))
	if direction == "" {
		direction = types.GraphDirectionOutgoing
	}
	return types.GraphEdge{
		ID:           identity,
		Source:       stringGraphProperty(props, "source"),
		Target:       stringGraphProperty(props, "target"),
		RelationType: relationType,
		Direction:    direction,
		Weight:       floatGraphProperty(props, "weight"),
	}
}

func graphEvidenceFromNode(node neo4j.Node) types.GraphEvidence {
	return types.GraphEvidence{
		ChunkID:            stringGraphProperty(node.Props, "chunk_id"),
		KnowledgeID:        stringGraphProperty(node.Props, "knowledge_id"),
		KnowledgeVersionID: stringGraphProperty(node.Props, "knowledge_version_id"),
		DocumentTitle:      stringGraphProperty(node.Props, "document_title"),
		Source:             stringGraphProperty(node.Props, "source"),
		ExtractorID:        stringGraphProperty(node.Props, "extractor_id"),
		Weight:             floatGraphProperty(node.Props, "weight"),
	}
}

func graphEvidenceIdentity(evidence types.GraphEvidence) string {
	return strings.Join([]string{evidence.KnowledgeID, evidence.KnowledgeVersionID, evidence.ChunkID, evidence.ExtractorID}, "|")
}

func appendUniqueGraphEvidence(existing []types.GraphEvidence, evidence types.GraphEvidence) []types.GraphEvidence {
	key := graphEvidenceIdentity(evidence)
	for _, item := range existing {
		if graphEvidenceIdentity(item) == key {
			return existing
		}
	}
	return append(existing, evidence)
}

func mergeCanonicalEdge(existing, incoming types.GraphEdge) types.GraphEdge {
	if existing.ID == "" {
		return incoming
	}
	for _, evidence := range incoming.Evidence {
		existing.Evidence = appendUniqueGraphEvidence(existing.Evidence, evidence)
	}
	return existing
}

func graphEdgeTargetForQuery(edge types.GraphEdge, current string, direction types.GraphDirection) (string, bool) {
	switch direction {
	case types.GraphDirectionOutgoing:
		return edge.Target, edge.Source == current
	case types.GraphDirectionIncoming:
		return edge.Source, edge.Target == current
	default:
		if edge.Source == current {
			return edge.Target, true
		}
		if edge.Target == current {
			return edge.Source, true
		}
		return "", false
	}
}

func uniqueStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	result := values[:0]
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func mapCanonicalEntities(values map[string]types.CanonicalEntity) []types.CanonicalEntity {
	result := make([]types.CanonicalEntity, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CanonicalKey < result[j].CanonicalKey })
	return result
}

func mapCanonicalEdges(values map[string]types.GraphEdge) []types.GraphEdge {
	result := make([]types.GraphEdge, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
