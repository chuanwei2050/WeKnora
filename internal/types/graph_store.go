package types

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// GraphSource identifies the authoritative extraction output that contributed
// an entity instance or relationship evidence. It is never inferred from node
// properties during rebuilds.
type GraphSource struct {
	KnowledgeID        string `json:"knowledge_id"`
	KnowledgeVersionID string `json:"knowledge_version_id,omitempty"`
	ChunkID            string `json:"chunk_id"`
	ExtractorID        string `json:"extractor_id"`
}

func (s GraphSource) evidenceKey() string {
	return strings.Join([]string{s.KnowledgeID, s.KnowledgeVersionID, s.ChunkID, s.ExtractorID}, "|")
}

type GraphConflict struct {
	Key    string   `json:"key"`
	Name   string   `json:"name"`
	Types  []string `json:"types"`
	Reason string   `json:"reason"`
}

type GraphRebuildResult struct {
	Namespace         string `json:"namespace"`
	EntityCount       int    `json:"entity_count"`
	InstanceCount     int    `json:"instance_count"`
	RelationshipCount int    `json:"relationship_count"`
	Switched          bool   `json:"switched"`
}

// GraphPublicationReadiness is supplied by the release coordinator after the
// vector, keyword and graph evidence checks have completed. A graph namespace
// cannot be promoted on graph readiness alone.
type GraphPublicationReadiness struct {
	VectorReady           bool
	KeywordReady          bool
	GraphReady            bool
	EvidenceVersionsValid bool
}

func (r GraphPublicationReadiness) Validate() error {
	if !r.VectorReady || !r.KeywordReady || !r.GraphReady || !r.EvidenceVersionsValid {
		return fmt.Errorf("all production indexes and evidence version checks must be ready")
	}
	return nil
}

// GraphRebuildRecord is the only accepted input for a full rebuild. Every
// instance and edge must carry its authoritative extraction source; callers
// cannot reconstruct relationship evidence from node properties.
type GraphRebuildRecord struct {
	Entity   *CanonicalEntity        `json:"entity,omitempty"`
	Instance *DocumentEntityInstance `json:"instance,omitempty"`
	Edge     *GraphEdge              `json:"edge,omitempty"`
	Source   GraphSource             `json:"source"`
}

type graphNamespace struct {
	entities  map[string]CanonicalEntity
	instances map[string]DocumentEntityInstance
	relations map[string]GraphEdge
	conflicts []GraphConflict
}

func newGraphNamespace() *graphNamespace {
	return &graphNamespace{entities: map[string]CanonicalEntity{}, instances: map[string]DocumentEntityInstance{}, relations: map[string]GraphEdge{}}
}

// GraphStore is a deterministic repository-grade in-memory implementation of
// the graph identity contract. A database adapter can persist the same keys;
// keeping the invariants here gives both adapters identical behavior.
type GraphStore struct {
	mu                 sync.RWMutex
	allowedTypes       map[string]bool
	allowedRelations   map[string]bool
	typeAliases        map[string]string
	namespaces         map[string]*graphNamespace
	activeNamespace    map[string]string
	previousNamespaces map[string][]string
}

func NewGraphStore(allowedTypes []string, aliases map[string]string) *GraphStore {
	return NewGraphStoreWithRelations(allowedTypes, aliases, nil)
}

// NewGraphStoreWithRelations adds the relation allowlist used by an
// extraction profile. A nil list keeps compatibility with legacy stores.
func NewGraphStoreWithRelations(allowedTypes []string, aliases map[string]string, allowedRelations []string) *GraphStore {
	store := &GraphStore{allowedTypes: map[string]bool{}, allowedRelations: map[string]bool{}, typeAliases: map[string]string{}, namespaces: map[string]*graphNamespace{}, activeNamespace: map[string]string{}, previousNamespaces: map[string][]string{}}
	for _, value := range allowedTypes {
		normalized := NormalizeEntityName(value)
		if normalized != "" {
			store.allowedTypes[normalized] = true
		}
	}
	for from, to := range aliases {
		from, to = NormalizeEntityName(from), NormalizeEntityName(to)
		if from != "" && to != "" {
			store.typeAliases[from] = to
		}
	}
	for _, relation := range allowedRelations {
		relation = normalizeRelationType(relation)
		if relation != "" {
			store.allowedRelations[relation] = true
		}
	}
	return store
}

func (s *GraphStore) namespace(tenantID uint64, knowledgeBaseID, namespace string) (*graphNamespace, error) {
	if tenantID == 0 || strings.TrimSpace(knowledgeBaseID) == "" || strings.TrimSpace(namespace) == "" {
		return nil, fmt.Errorf("tenant, knowledge base and namespace are required")
	}
	key := fmt.Sprintf("%d:%s:%s", tenantID, NormalizeEntityName(knowledgeBaseID), namespace)
	item := s.namespaces[key]
	if item == nil {
		item = newGraphNamespace()
		s.namespaces[key] = item
	}
	return item, nil
}

func (s *GraphStore) normalizeEntityType(value string) (string, error) {
	typ := NormalizeEntityName(value)
	if alias, ok := s.typeAliases[typ]; ok {
		typ = alias
	}
	if typ == "" {
		return "", fmt.Errorf("entity type is required")
	}
	if len(s.allowedTypes) > 0 && !s.allowedTypes[typ] {
		return "", fmt.Errorf("unknown entity type %q", value)
	}
	return typ, nil
}

func (s *GraphStore) AddEntity(tenantID uint64, knowledgeBaseID, namespace string, entity CanonicalEntity) (CanonicalEntity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ns, err := s.namespace(tenantID, knowledgeBaseID, namespace)
	if err != nil {
		return CanonicalEntity{}, err
	}
	typ, err := s.normalizeEntityType(entity.EntityType)
	if err != nil {
		return CanonicalEntity{}, err
	}
	name := NormalizeEntityName(entity.Name)
	if name == "" {
		return CanonicalEntity{}, fmt.Errorf("entity name is required")
	}
	key := CanonicalEntityKey(tenantID, knowledgeBaseID, typ, name)
	if entity.CanonicalKey != "" && entity.CanonicalKey != key {
		return CanonicalEntity{}, fmt.Errorf("canonical key does not match normalized identity")
	}
	entity.CanonicalKey, entity.Name, entity.EntityType = key, name, typ
	entity.TenantID, entity.KnowledgeBaseID = tenantID, knowledgeBaseID
	for _, existing := range ns.entities {
		if existing.Name != name || existing.EntityType == typ {
			continue
		}
		conflictKey := strings.Join([]string{tenantIDString(tenantID), NormalizeEntityName(knowledgeBaseID), name}, "|")
		conflictFound := false
		for index := range ns.conflicts {
			if ns.conflicts[index].Key == conflictKey {
				ns.conflicts[index].Types = mergeStrings(ns.conflicts[index].Types, []string{existing.EntityType, typ})
				conflictFound = true
				break
			}
		}
		if !conflictFound {
			ns.conflicts = append(ns.conflicts, GraphConflict{Key: conflictKey, Name: name, Types: mergeStrings(nil, []string{existing.EntityType, typ}), Reason: "same normalized name has multiple entity types"})
		}
	}
	if existing, ok := ns.entities[key]; ok {
		entity.Aliases = mergeStrings(existing.Aliases, entity.Aliases)
		entity.Evidence = mergeEvidence(existing.Evidence, entity.Evidence)
	}
	ns.entities[key] = entity
	return entity, nil
}

func (s *GraphStore) AddInstance(tenantID uint64, knowledgeBaseID, namespace string, instance DocumentEntityInstance, source GraphSource) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ns, err := s.namespace(tenantID, knowledgeBaseID, namespace)
	if err != nil {
		return err
	}
	typ, err := s.normalizeEntityType(instance.EntityType)
	if err != nil {
		return err
	}
	if instance.KnowledgeID == "" || source.KnowledgeID == "" || source.ChunkID == "" {
		return fmt.Errorf("instance and source knowledge/chunk identity are required")
	}
	key := CanonicalEntityKey(tenantID, knowledgeBaseID, typ, instance.Name)
	if instance.CanonicalKey != "" && instance.CanonicalKey != key {
		return fmt.Errorf("instance canonical key does not match normalized identity")
	}
	if _, ok := ns.entities[key]; !ok {
		return fmt.Errorf("canonical entity %q does not exist", key)
	}
	instance.CanonicalKey, instance.EntityType = key, typ
	instance.ChunkIDs = mergeStrings(instance.ChunkIDs, []string{source.ChunkID})
	instance.Sources = mergeGraphSources(instance.Sources, []GraphSource{source})
	instanceKey := strings.Join([]string{key, instance.KnowledgeID}, "|")
	if previous, ok := ns.instances[instanceKey]; ok {
		instance.ChunkIDs = mergeStrings(previous.ChunkIDs, instance.ChunkIDs)
		instance.Sources = mergeGraphSources(previous.Sources, instance.Sources)
	}
	ns.instances[instanceKey] = instance
	return nil
}

func (s *GraphStore) AddRelationshipEvidence(tenantID uint64, knowledgeBaseID, namespace string, edge GraphEdge, source GraphSource) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ns, err := s.namespace(tenantID, knowledgeBaseID, namespace)
	if err != nil {
		return err
	}
	if edge.Source == "" || edge.Target == "" || edge.RelationType == "" || source.KnowledgeID == "" || source.ChunkID == "" {
		return fmt.Errorf("relationship and source identity are required")
	}
	edge.RelationType = normalizeRelationType(edge.RelationType)
	if len(s.allowedRelations) > 0 && !s.allowedRelations[edge.RelationType] {
		return fmt.Errorf("unknown relation type %q", edge.RelationType)
	}
	if _, ok := ns.entities[edge.Source]; !ok {
		return fmt.Errorf("source entity %q does not exist", edge.Source)
	}
	if _, ok := ns.entities[edge.Target]; !ok {
		return fmt.Errorf("target entity %q does not exist", edge.Target)
	}
	if edge.Direction == "" {
		edge.Direction = GraphDirectionOutgoing
	}
	if edge.ID == "" {
		edge.ID = strings.Join([]string{edge.Source, edge.RelationType, string(edge.Direction), edge.Target}, "|")
	}
	item := ns.relations[edge.ID]
	item.ID, item.Source, item.Target, item.RelationType, item.Direction = edge.ID, edge.Source, edge.Target, edge.RelationType, edge.Direction
	if edge.Weight != 0 {
		item.Weight = edge.Weight
	}
	item.Evidence = mergeEvidence(item.Evidence, []GraphEvidence{{ChunkID: source.ChunkID, KnowledgeID: source.KnowledgeID, KnowledgeVersionID: source.KnowledgeVersionID, ExtractorID: source.ExtractorID, Weight: sourceWeight(edge.Weight)}})
	ns.relations[edge.ID] = item
	return nil
}

func sourceWeight(weight float64) float64 {
	if weight == 0 {
		return .5
	}
	return weight
}

func normalizeRelationType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// RemoveSource removes only evidence contributed by one extraction source;
// shared entities and edges survive while other evidence remains.
func (s *GraphStore) RemoveSource(tenantID uint64, knowledgeBaseID, namespace string, source GraphSource) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ns, err := s.namespace(tenantID, knowledgeBaseID, namespace)
	if err != nil {
		return err
	}
	for key, edge := range ns.relations {
		edge.Evidence = removeEvidence(edge.Evidence, source)
		if len(edge.Evidence) == 0 {
			delete(ns.relations, key)
		} else {
			ns.relations[key] = edge
		}
	}
	for key, instance := range ns.instances {
		instance.Sources = removeGraphSource(instance.Sources, source)
		instance.ChunkIDs = nil
		for _, item := range instance.Sources {
			instance.ChunkIDs = mergeStrings(instance.ChunkIDs, []string{item.ChunkID})
		}
		if len(instance.ChunkIDs) == 0 {
			delete(ns.instances, key)
		} else {
			ns.instances[key] = instance
		}
	}
	for key, entity := range ns.entities {
		entity.Evidence = removeEvidence(entity.Evidence, source)
		ns.entities[key] = entity
	}
	return nil
}

// Rebuild writes an isolated namespace from authoritative entities and edges,
// then optionally switches the active namespace only after all records pass.
func (s *GraphStore) Rebuild(ctx context.Context, tenantID uint64, knowledgeBaseID, namespace string, entities []CanonicalEntity, edges []GraphEdge, sources map[string]GraphSource, switchActive bool) (GraphRebuildResult, error) {
	records := make([]GraphRebuildRecord, 0, len(entities)+len(edges))
	for index := range entities {
		entity := entities[index]
		records = append(records, GraphRebuildRecord{Entity: &entity})
	}
	for index := range edges {
		edge := edges[index]
		source, ok := sources[edge.ID]
		if !ok {
			return GraphRebuildResult{}, fmt.Errorf("relationship %q has no authoritative source", edge.ID)
		}
		records = append(records, GraphRebuildRecord{Edge: &edge, Source: source})
	}
	return s.RebuildFromRecords(ctx, tenantID, knowledgeBaseID, namespace, records, switchActive)
}

func (s *GraphStore) RebuildFromRecords(ctx context.Context, tenantID uint64, knowledgeBaseID, namespace string, records []GraphRebuildRecord, switchActive bool) (GraphRebuildResult, error) {
	if err := ctx.Err(); err != nil {
		return GraphRebuildResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if tenantID == 0 || strings.TrimSpace(knowledgeBaseID) == "" || strings.TrimSpace(namespace) == "" {
		return GraphRebuildResult{}, fmt.Errorf("tenant, knowledge base and namespace are required")
	}
	ns := newGraphNamespace()
	for _, record := range records {
		if record.Entity == nil {
			continue
		}
		entity := *record.Entity
		typ, typErr := s.normalizeEntityType(entity.EntityType)
		if typErr != nil {
			return GraphRebuildResult{}, typErr
		}
		entity.EntityType = typ
		entity.CanonicalKey = CanonicalEntityKey(tenantID, knowledgeBaseID, typ, entity.Name)
		entity.TenantID, entity.KnowledgeBaseID = tenantID, knowledgeBaseID
		if existing, ok := ns.entities[entity.CanonicalKey]; ok {
			entity.Aliases = mergeStrings(existing.Aliases, entity.Aliases)
			entity.Evidence = mergeEvidence(existing.Evidence, entity.Evidence)
		}
		ns.entities[entity.CanonicalKey] = entity
	}
	for _, record := range records {
		if record.Instance != nil {
			if err := addInstanceToNamespace(ns, *record.Instance, record.Source, tenantID, knowledgeBaseID, s); err != nil {
				return GraphRebuildResult{}, err
			}
		}
		if record.Edge != nil {
			edge := *record.Edge
			if len(s.allowedRelations) > 0 && !s.allowedRelations[normalizeRelationType(edge.RelationType)] {
				return GraphRebuildResult{}, fmt.Errorf("unknown relation type %q", edge.RelationType)
			}
			if record.Source.KnowledgeID == "" || record.Source.ChunkID == "" {
				return GraphRebuildResult{}, fmt.Errorf("relationship %q has no authoritative source", edge.ID)
			}
			if err := addEdgeToNamespace(ns, edge, record.Source); err != nil {
				return GraphRebuildResult{}, err
			}
		}
	}
	key := fmt.Sprintf("%d:%s:%s", tenantID, NormalizeEntityName(knowledgeBaseID), namespace)
	s.namespaces[key] = ns
	if switchActive {
		activeKey := fmt.Sprintf("%d:%s", tenantID, NormalizeEntityName(knowledgeBaseID))
		if previous := s.activeNamespace[activeKey]; previous != "" && previous != namespace {
			s.previousNamespaces[activeKey] = append(s.previousNamespaces[activeKey], previous)
		}
		s.activeNamespace[activeKey] = namespace
	}
	return GraphRebuildResult{Namespace: namespace, EntityCount: len(ns.entities), InstanceCount: len(ns.instances), RelationshipCount: len(ns.relations), Switched: switchActive}, nil
}

func addInstanceToNamespace(ns *graphNamespace, instance DocumentEntityInstance, source GraphSource, tenantID uint64, knowledgeBaseID string, store *GraphStore) error {
	typ, err := store.normalizeEntityType(instance.EntityType)
	if err != nil {
		return err
	}
	if instance.KnowledgeID == "" || source.KnowledgeID == "" || source.ChunkID == "" {
		return fmt.Errorf("instance and source knowledge/chunk identity are required")
	}
	key := CanonicalEntityKey(tenantID, knowledgeBaseID, typ, instance.Name)
	if _, ok := ns.entities[key]; !ok {
		return fmt.Errorf("canonical entity %q does not exist", key)
	}
	instance.CanonicalKey, instance.EntityType = key, typ
	instance.ChunkIDs = mergeStrings(instance.ChunkIDs, []string{source.ChunkID})
	instance.Sources = mergeGraphSources(instance.Sources, []GraphSource{source})
	instanceKey := strings.Join([]string{key, instance.KnowledgeID}, "|")
	if previous, ok := ns.instances[instanceKey]; ok {
		instance.ChunkIDs = mergeStrings(previous.ChunkIDs, instance.ChunkIDs)
		instance.Sources = mergeGraphSources(previous.Sources, instance.Sources)
	}
	ns.instances[instanceKey] = instance
	return nil
}

func addEdgeToNamespace(ns *graphNamespace, edge GraphEdge, source GraphSource) error {
	if _, ok := ns.entities[edge.Source]; !ok {
		return fmt.Errorf("source entity %q does not exist", edge.Source)
	}
	if _, ok := ns.entities[edge.Target]; !ok {
		return fmt.Errorf("target entity %q does not exist", edge.Target)
	}
	edge.RelationType = normalizeRelationType(edge.RelationType)
	if edge.Direction == "" {
		edge.Direction = GraphDirectionOutgoing
	}
	if edge.ID == "" {
		edge.ID = strings.Join([]string{edge.Source, edge.RelationType, string(edge.Direction), edge.Target}, "|")
	}
	existing := ns.relations[edge.ID]
	edge.Evidence = mergeEvidence(existing.Evidence, []GraphEvidence{{ChunkID: source.ChunkID, KnowledgeID: source.KnowledgeID, KnowledgeVersionID: source.KnowledgeVersionID, ExtractorID: source.ExtractorID, Weight: sourceWeight(edge.Weight)}})
	if existing.Weight != 0 && edge.Weight == 0 {
		edge.Weight = existing.Weight
	}
	ns.relations[edge.ID] = edge
	return nil
}

func tenantIDString(tenantID uint64) string {
	return fmt.Sprintf("%d", tenantID)
}

func (s *GraphStore) Query(ctx context.Context, query GraphQuery, allowedRelations []string) (GraphSearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	query.Seeds = append([]GraphSeed(nil), query.Seeds...)
	for index := range query.Seeds {
		seedType := NormalizeEntityName(query.Seeds[index].EntityType)
		if alias, ok := s.typeAliases[seedType]; ok {
			query.Seeds[index].EntityType = alias
		}
	}
	key := fmt.Sprintf("%d:%s", query.Scope.TenantID, NormalizeEntityName(query.Scope.KnowledgeBaseID))
	namespace := s.activeNamespace[key]
	if namespace == "" {
		namespace = "default"
	}
	ns, err := s.namespaceRead(query.Scope.TenantID, query.Scope.KnowledgeBaseID, namespace)
	if err != nil {
		return GraphSearchResult{}, err
	}
	nodes := make([]CanonicalEntity, 0, len(ns.entities))
	for _, node := range ns.entities {
		nodes = append(nodes, node)
	}
	edges := make([]GraphEdge, 0, len(ns.relations))
	for _, edge := range ns.relations {
		edges = append(edges, edge)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].CanonicalKey < nodes[j].CanonicalKey })
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	return TraverseGraph(ctx, query, nodes, edges, allowedRelations)
}

func (s *GraphStore) namespaceRead(tenantID uint64, knowledgeBaseID, namespace string) (*graphNamespace, error) {
	key := fmt.Sprintf("%d:%s:%s", tenantID, NormalizeEntityName(knowledgeBaseID), namespace)
	item := s.namespaces[key]
	if item == nil {
		return nil, fmt.Errorf("graph namespace %q does not exist", namespace)
	}
	return item, nil
}

func (s *GraphStore) ActiveNamespace(tenantID uint64, knowledgeBaseID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeNamespace[fmt.Sprintf("%d:%s", tenantID, NormalizeEntityName(knowledgeBaseID))]
}

// Conflicts returns the ambiguity records captured for one graph namespace.
// Callers receive a copy so the store remains the source of truth.
func (s *GraphStore) Conflicts(tenantID uint64, knowledgeBaseID, namespace string) ([]GraphConflict, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ns, err := s.namespaceRead(tenantID, knowledgeBaseID, namespace)
	if err != nil {
		return nil, err
	}
	return append([]GraphConflict(nil), ns.conflicts...), nil
}

// SwitchNamespace atomically changes the active graph only after the target
// namespace has been fully materialized.
func (s *GraphStore) SwitchNamespace(tenantID uint64, knowledgeBaseID, namespace string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%d:%s", tenantID, NormalizeEntityName(knowledgeBaseID))
	namespaceKey := fmt.Sprintf("%d:%s:%s", tenantID, NormalizeEntityName(knowledgeBaseID), namespace)
	if s.namespaces[namespaceKey] == nil {
		return fmt.Errorf("graph namespace %q does not exist", namespace)
	}
	if previous := s.activeNamespace[key]; previous != "" && previous != namespace {
		s.previousNamespaces[key] = append(s.previousNamespaces[key], previous)
	}
	s.activeNamespace[key] = namespace
	return nil
}

// PublishNamespace promotes a fully built staging namespace only after all
// index readiness checks pass. The copy and active-pointer switch happen under
// one lock, so readers observe either the previous graph or the complete new
// graph, never a partially populated namespace.
func (s *GraphStore) PublishNamespace(tenantID uint64, knowledgeBaseID, stagingNamespace, activeNamespace string, readiness GraphPublicationReadiness) error {
	if err := readiness.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(stagingNamespace) == "" || strings.TrimSpace(activeNamespace) == "" {
		return fmt.Errorf("staging and active namespaces are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stagingKey := fmt.Sprintf("%d:%s:%s", tenantID, NormalizeEntityName(knowledgeBaseID), stagingNamespace)
	staging := s.namespaces[stagingKey]
	if staging == nil {
		return fmt.Errorf("graph namespace %q does not exist", stagingNamespace)
	}
	activeKey := fmt.Sprintf("%d:%s", tenantID, NormalizeEntityName(knowledgeBaseID))
	activeNamespaceKey := fmt.Sprintf("%d:%s:%s", tenantID, NormalizeEntityName(knowledgeBaseID), activeNamespace)
	s.namespaces[activeNamespaceKey] = cloneGraphNamespace(staging)
	if previous := s.activeNamespace[activeKey]; previous != "" && previous != activeNamespace {
		s.previousNamespaces[activeKey] = append(s.previousNamespaces[activeKey], previous)
	}
	s.activeNamespace[activeKey] = activeNamespace
	return nil
}

// RollbackNamespace returns to the most recent previously active namespace.
func (s *GraphStore) RollbackNamespace(tenantID uint64, knowledgeBaseID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%d:%s", tenantID, NormalizeEntityName(knowledgeBaseID))
	history := s.previousNamespaces[key]
	if len(history) == 0 {
		return "", fmt.Errorf("graph namespace rollback history is empty")
	}
	previous := history[len(history)-1]
	s.previousNamespaces[key] = history[:len(history)-1]
	if s.namespaces[fmt.Sprintf("%d:%s:%s", tenantID, NormalizeEntityName(knowledgeBaseID), previous)] == nil {
		return "", fmt.Errorf("graph namespace %q no longer exists", previous)
	}
	s.activeNamespace[key] = previous
	return previous, nil
}

func cloneGraphNamespace(source *graphNamespace) *graphNamespace {
	if source == nil {
		return nil
	}
	clone := newGraphNamespace()
	for key, value := range source.entities {
		value.Aliases = append([]string(nil), value.Aliases...)
		value.Evidence = append([]GraphEvidence(nil), value.Evidence...)
		clone.entities[key] = value
	}
	for key, value := range source.instances {
		value.ChunkIDs = append([]string(nil), value.ChunkIDs...)
		value.Sources = append([]GraphSource(nil), value.Sources...)
		clone.instances[key] = value
	}
	for key, value := range source.relations {
		value.Evidence = append([]GraphEvidence(nil), value.Evidence...)
		clone.relations[key] = value
	}
	clone.conflicts = append([]GraphConflict(nil), source.conflicts...)
	return clone
}

func mergeStrings(left, right []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(left)+len(right))
	for _, value := range append(append([]string(nil), left...), right...) {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func mergeEvidence(left, right []GraphEvidence) []GraphEvidence {
	seen := map[string]bool{}
	result := make([]GraphEvidence, 0, len(left)+len(right))
	for _, value := range append(append([]GraphEvidence(nil), left...), right...) {
		key := value.KnowledgeID + "|" + value.KnowledgeVersionID + "|" + value.ChunkID + "|" + value.ExtractorID
		if value.ChunkID != "" && !seen[key] {
			seen[key] = true
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].KnowledgeID+result[i].ChunkID < result[j].KnowledgeID+result[j].ChunkID
	})
	return result
}

func removeEvidence(values []GraphEvidence, source GraphSource) []GraphEvidence {
	result := make([]GraphEvidence, 0, len(values))
	for _, value := range values {
		if value.KnowledgeID == source.KnowledgeID && value.KnowledgeVersionID == source.KnowledgeVersionID && value.ChunkID == source.ChunkID && (source.ExtractorID == "" || value.ExtractorID == source.ExtractorID) {
			continue
		}
		result = append(result, value)
	}
	return result
}

func removeString(values []string, target string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != target {
			result = append(result, value)
		}
	}
	return result
}

func mergeGraphSources(left, right []GraphSource) []GraphSource {
	seen := map[string]bool{}
	result := make([]GraphSource, 0, len(left)+len(right))
	for _, source := range append(append([]GraphSource(nil), left...), right...) {
		if source.KnowledgeID == "" || source.ChunkID == "" || seen[source.evidenceKey()] {
			continue
		}
		seen[source.evidenceKey()] = true
		result = append(result, source)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].evidenceKey() < result[j].evidenceKey() })
	return result
}

func removeGraphSource(values []GraphSource, source GraphSource) []GraphSource {
	result := make([]GraphSource, 0, len(values))
	for _, value := range values {
		if value.evidenceKey() == source.evidenceKey() {
			continue
		}
		result = append(result, value)
	}
	return result
}
