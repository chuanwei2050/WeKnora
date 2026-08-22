package types

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
)

type GraphDirection string

const (
	GraphDirectionOutgoing GraphDirection = "outgoing"
	GraphDirectionIncoming GraphDirection = "incoming"
	GraphDirectionBoth     GraphDirection = "both"
)

type GraphScope struct {
	TenantID                 uint64            `json:"tenant_id"`
	KnowledgeBaseID          string            `json:"knowledge_base_id"`
	AllowedKnowledgeIDs      []string          `json:"-"`
	CurrentKnowledgeVersions map[string]string `json:"-"`
}

// GraphNamespaceForVersion gives governed graph writes an isolated target.
// The active pointer is switched only after the complete version is ready;
// ungoverned callers retain the legacy default namespace.
func GraphNamespaceForVersion(versionID string, staging bool) string {
	versionID = strings.TrimSpace(versionID)
	if versionID == "" {
		return "default"
	}
	if staging {
		return "staging:" + versionID
	}
	return "active:" + versionID
}

type GraphSeed struct {
	Name         string `json:"name"`
	EntityType   string `json:"entity_type,omitempty"`
	CanonicalKey string `json:"canonical_key,omitempty"`
}

type GraphQuery struct {
	Scope            GraphScope     `json:"scope"`
	Seeds            []GraphSeed    `json:"seeds"`
	RelationTypes    []string       `json:"relation_types,omitempty"`
	Direction        GraphDirection `json:"direction,omitempty"`
	MaxDepth         int            `json:"max_depth"`
	BranchFactor     int            `json:"branch_factor"`
	MaxExpandedNodes int            `json:"max_expanded_nodes"`
	MaxPaths         int            `json:"max_paths"`
	Timeout          time.Duration  `json:"timeout"`
	MinPathScore     float64        `json:"min_path_score"`
}

type CanonicalEntity struct {
	CanonicalKey    string          `json:"canonical_key"`
	Name            string          `json:"name"`
	EntityType      string          `json:"entity_type"`
	TenantID        uint64          `json:"tenant_id"`
	KnowledgeBaseID string          `json:"knowledge_base_id"`
	Aliases         []string        `json:"aliases,omitempty"`
	Evidence        []GraphEvidence `json:"evidence,omitempty"`
}

type DocumentEntityInstance struct {
	CanonicalKey string        `json:"canonical_key"`
	Name         string        `json:"name"`
	EntityType   string        `json:"entity_type"`
	KnowledgeID  string        `json:"knowledge_id"`
	ChunkIDs     []string      `json:"chunk_ids,omitempty"`
	Sources      []GraphSource `json:"sources,omitempty"`
}

type GraphEvidence struct {
	ChunkID            string  `json:"chunk_id"`
	KnowledgeID        string  `json:"knowledge_id"`
	KnowledgeVersionID string  `json:"knowledge_version_id,omitempty"`
	DocumentTitle      string  `json:"document_title,omitempty"`
	Source             string  `json:"source,omitempty"`
	ExtractorID        string  `json:"extractor_id,omitempty"`
	Weight             float64 `json:"weight,omitempty"`
}

type GraphEdge struct {
	ID           string          `json:"id"`
	Source       string          `json:"source"`
	Target       string          `json:"target"`
	RelationType string          `json:"relation_type"`
	Direction    GraphDirection  `json:"direction"`
	Weight       float64         `json:"weight"`
	Evidence     []GraphEvidence `json:"evidence"`
}

type GraphPath struct {
	NodeKeys []string        `json:"node_keys"`
	Edges    []GraphEdge     `json:"edges"`
	Score    float64         `json:"score"`
	Evidence []GraphEvidence `json:"evidence"`
}

type GraphSearchResult struct {
	Nodes            []CanonicalEntity `json:"nodes"`
	Edges            []GraphEdge       `json:"edges"`
	Paths            []GraphPath       `json:"paths"`
	Citations        []GraphEvidence   `json:"citations"`
	Truncated        bool              `json:"truncated"`
	TruncationReason string            `json:"truncation_reason,omitempty"`
	Fallback         bool              `json:"fallback"`
	FallbackReason   string            `json:"fallback_reason,omitempty"`
}

// EnsureGraphCitations backfills the canonical citation projection from path
// evidence when a repository returns only paths. It is idempotent and keeps
// citation identity stable across adapters.
func EnsureGraphCitations(result *GraphSearchResult) {
	if result == nil {
		return
	}
	seen := make(map[string]bool, len(result.Citations))
	for _, citation := range result.Citations {
		seen[citation.KnowledgeID+":"+citation.ChunkID] = true
	}
	for _, path := range result.Paths {
		for _, citation := range path.Evidence {
			key := citation.KnowledgeID + ":" + citation.ChunkID
			if citation.ChunkID != "" && !seen[key] {
				seen[key] = true
				result.Citations = append(result.Citations, citation)
			}
		}
	}
}

// FilterGraphSearchResult removes evidence from non-current governed
// versions before graph data, context, ordering or citations are consumed.
// A path that loses an edge's evidence is removed as a whole; a mixed-version
// path must never be partially presented as a valid relationship chain.
func FilterGraphSearchResult(result *GraphSearchResult, currentVersions map[string]string) {
	if result == nil || len(currentVersions) == 0 {
		return
	}
	validEvidence := func(values []GraphEvidence) []GraphEvidence {
		filtered := make([]GraphEvidence, 0, len(values))
		for _, value := range values {
			if current, governed := currentVersions[value.KnowledgeID]; governed && value.KnowledgeVersionID != current {
				continue
			}
			filtered = append(filtered, value)
		}
		return filtered
	}
	for index := range result.Nodes {
		result.Nodes[index].Evidence = validEvidence(result.Nodes[index].Evidence)
	}
	validEdges := make(map[string]bool, len(result.Edges))
	filteredEdges := result.Edges[:0]
	for index := range result.Edges {
		result.Edges[index].Evidence = validEvidence(result.Edges[index].Evidence)
		if len(result.Edges[index].Evidence) == 0 {
			continue
		}
		validEdges[result.Edges[index].ID] = true
		filteredEdges = append(filteredEdges, result.Edges[index])
	}
	result.Edges = filteredEdges
	filteredPaths := result.Paths[:0]
	for index := range result.Paths {
		path := result.Paths[index]
		path.Evidence = validEvidence(path.Evidence)
		valid := len(path.Evidence) > 0
		for edgeIndex := range path.Edges {
			path.Edges[edgeIndex].Evidence = validEvidence(path.Edges[edgeIndex].Evidence)
			if len(path.Edges[edgeIndex].Evidence) == 0 || !validEdges[path.Edges[edgeIndex].ID] {
				valid = false
			}
		}
		if valid {
			filteredPaths = append(filteredPaths, path)
		}
	}
	result.Paths = filteredPaths
	usedNodes := make(map[string]bool)
	for _, path := range result.Paths {
		for _, key := range path.NodeKeys {
			usedNodes[key] = true
		}
	}
	filteredNodes := result.Nodes[:0]
	for _, node := range result.Nodes {
		if usedNodes[node.CanonicalKey] || len(node.Evidence) > 0 {
			filteredNodes = append(filteredNodes, node)
		}
	}
	result.Nodes = filteredNodes
	filteredCitations := make([]GraphEvidence, 0, len(result.Citations))
	for _, citation := range result.Citations {
		if values := validEvidence([]GraphEvidence{citation}); len(values) > 0 {
			filteredCitations = append(filteredCitations, citation)
		}
	}
	result.Citations = filteredCitations
	EnsureGraphCitations(result)
}

// RenderGraphContext serializes only authorized structured evidence and keeps
// a hard character budget before it reaches the answer model.
func RenderGraphContext(result *GraphSearchResult, maxChars int) string {
	if result == nil || maxChars <= 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("<graph_evidence>\n")
	for index, path := range result.Paths {
		line := fmt.Sprintf("<path index=\"%d\" score=\"%.4f\" nodes=\"%s\">", index+1, path.Score, strings.Join(path.NodeKeys, " -> "))
		if builder.Len()+len(line)+1 >= maxChars {
			break
		}
		builder.WriteString(line)
		for _, edge := range path.Edges {
			builder.WriteString(fmt.Sprintf("<relation source=\"%s\" target=\"%s\" type=\"%s\"/>", edge.Source, edge.Target, edge.RelationType))
		}
		for _, evidence := range path.Evidence {
			builder.WriteString(fmt.Sprintf("<citation knowledge_id=\"%s\" chunk_id=\"%s\" version_id=\"%s\"/>", evidence.KnowledgeID, evidence.ChunkID, evidence.KnowledgeVersionID))
		}
		builder.WriteString("</path>\n")
	}
	builder.WriteString("</graph_evidence>")
	value := builder.String()
	if len([]rune(value)) <= maxChars {
		return value
	}
	return string([]rune(value)[:maxChars]) + "\n</graph_evidence>"
}

type GraphTextFusionConfig struct {
	GraphWeight float64 `json:"graph_weight"`
	TextWeight  float64 `json:"text_weight"`
	MaxResults  int     `json:"max_results"`
}

func (c GraphTextFusionConfig) Validate() error {
	if c.GraphWeight < 0 || c.TextWeight < 0 || c.GraphWeight+c.TextWeight <= 0 {
		return fmt.Errorf("graph and text weights must be non-negative and non-zero")
	}
	if c.MaxResults < 0 || c.MaxResults > 1000 {
		return fmt.Errorf("max_results is out of range")
	}
	return nil
}

type FusedGraphEvidence struct {
	Evidence GraphEvidence `json:"evidence"`
	Score    float64       `json:"score"`
	PathKeys []string      `json:"path_keys,omitempty"`
}

// FuseGraphAndTextScores gives graph paths and text hits one deterministic
// ranking while retaining the evidence link used to explain each score.
func FuseGraphAndTextScores(graph GraphSearchResult, textResults []*SearchResult, config GraphTextFusionConfig) ([]FusedGraphEvidence, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	textScores := map[string]float64{}
	for _, result := range textResults {
		if result == nil {
			continue
		}
		if result.Score > textScores[result.ID] {
			textScores[result.ID] = result.Score
		}
	}
	items := make([]FusedGraphEvidence, 0)
	itemIndex := make(map[string]int)
	merge := func(evidence GraphEvidence, score float64, pathKeys []string) {
		score = math.Round(score*1e12) / 1e12
		key := evidence.ChunkID
		if index, ok := itemIndex[key]; ok {
			if score > items[index].Score {
				items[index].Score = score
				items[index].PathKeys = append([]string(nil), pathKeys...)
			}
			return
		}
		itemIndex[key] = len(items)
		items = append(items, FusedGraphEvidence{Evidence: evidence, Score: score, PathKeys: append([]string(nil), pathKeys...)})
	}
	for _, path := range graph.Paths {
		for _, evidence := range path.Evidence {
			textScore := textScores[evidence.ChunkID]
			merge(evidence, config.GraphWeight*path.Score+config.TextWeight*textScore, path.NodeKeys)
		}
	}
	for _, evidence := range graph.Citations {
		merge(evidence, config.TextWeight*textScores[evidence.ChunkID], nil)
	}
	for _, result := range textResults {
		if result == nil || result.ID == "" {
			continue
		}
		merge(GraphEvidence{ChunkID: result.ID, KnowledgeID: result.KnowledgeID, KnowledgeVersionID: result.KnowledgeVersionID, DocumentTitle: result.KnowledgeTitle, Source: result.KnowledgeTitle}, config.TextWeight*result.Score, nil)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Score == items[j].Score {
			return items[i].Evidence.KnowledgeID+items[i].Evidence.ChunkID < items[j].Evidence.KnowledgeID+items[j].Evidence.ChunkID
		}
		return items[i].Score > items[j].Score
	})
	if config.MaxResults > 0 && len(items) > config.MaxResults {
		items = items[:config.MaxResults]
	}
	return items, nil
}

func (s GraphScope) Validate() error {
	if s.TenantID == 0 {
		return fmt.Errorf("tenant_id is required")
	}
	if strings.TrimSpace(s.KnowledgeBaseID) == "" {
		return fmt.Errorf("knowledge_base_id is required")
	}
	return nil
}

func (q GraphQuery) Validate(allowedRelations []string) error {
	if err := q.Scope.Validate(); err != nil {
		return err
	}
	if len(q.Seeds) == 0 {
		return fmt.Errorf("at least one graph seed is required")
	}
	if q.MaxDepth == 0 {
		q.MaxDepth = 2
	}
	if q.MaxDepth < 1 || q.MaxDepth > 4 {
		return fmt.Errorf("max_depth must be between 1 and 4")
	}
	if q.BranchFactor < 0 || q.BranchFactor > 100 {
		return fmt.Errorf("branch_factor must be between 0 and 100")
	}
	if q.MaxExpandedNodes < 0 || q.MaxExpandedNodes > 10000 {
		return fmt.Errorf("max_expanded_nodes is out of range")
	}
	if q.MaxPaths < 0 || q.MaxPaths > 1000 {
		return fmt.Errorf("max_paths is out of range")
	}
	if q.Timeout < 0 || q.Timeout > 30*time.Second {
		return fmt.Errorf("timeout must be between 0 and 30 seconds")
	}
	if q.Direction != "" && q.Direction != GraphDirectionOutgoing && q.Direction != GraphDirectionIncoming && q.Direction != GraphDirectionBoth {
		return fmt.Errorf("unknown graph direction %q", q.Direction)
	}
	allowed := make(map[string]struct{}, len(allowedRelations))
	for _, relation := range allowedRelations {
		allowed[relation] = struct{}{}
	}
	for _, relation := range q.RelationTypes {
		if _, ok := allowed[relation]; !ok {
			return fmt.Errorf("relation type %q is not allowed", relation)
		}
	}
	return nil
}

func NormalizeEntityName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastSpace := false
	for _, r := range value {
		if unicode.IsSpace(r) || r == '_' || r == '-' {
			if !lastSpace {
				b.WriteRune(' ')
			}
			lastSpace = true
			continue
		}
		if unicode.IsPunct(r) {
			continue
		}
		b.WriteRune(r)
		lastSpace = false
	}
	return strings.TrimSpace(b.String())
}

func CanonicalEntityKey(tenantID uint64, knowledgeBaseID, entityType, name string) string {
	return fmt.Sprintf("%d:%s:%s:%s", tenantID, NormalizeEntityName(knowledgeBaseID), NormalizeEntityName(entityType), NormalizeEntityName(name))
}

// TraverseGraph is an implementation-independent bounded traversal used by
// Neo4j adapters and repository tests. The caller supplies already authorized
// graph records; this function never broadens the scope.
func TraverseGraph(ctx context.Context, query GraphQuery, nodes []CanonicalEntity, edges []GraphEdge, allowedRelations []string) (GraphSearchResult, error) {
	if err := query.Validate(allowedRelations); err != nil {
		return GraphSearchResult{}, err
	}
	if query.Direction == "" {
		query.Direction = GraphDirectionBoth
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
	ctx, cancel := context.WithTimeout(ctx, query.Timeout)
	defer cancel()

	allowedKnowledge := make(map[string]struct{}, len(query.Scope.AllowedKnowledgeIDs))
	for _, id := range query.Scope.AllowedKnowledgeIDs {
		allowedKnowledge[id] = struct{}{}
	}
	currentVersions := query.Scope.CurrentKnowledgeVersions
	allowedTypes := make(map[string]struct{}, len(query.RelationTypes))
	for _, typ := range query.RelationTypes {
		allowedTypes[typ] = struct{}{}
	}
	findSeed := func(seed GraphSeed, node CanonicalEntity) bool {
		if seed.CanonicalKey != "" {
			return seed.CanonicalKey == node.CanonicalKey
		}
		return NormalizeEntityName(seed.Name) == NormalizeEntityName(node.Name) &&
			(seed.EntityType == "" || NormalizeEntityName(seed.EntityType) == NormalizeEntityName(node.EntityType))
	}
	seedKeys := make(map[string]struct{})
	for _, node := range nodes {
		for _, seed := range query.Seeds {
			if findSeed(seed, node) {
				seedKeys[node.CanonicalKey] = struct{}{}
			}
		}
	}
	if len(seedKeys) == 0 {
		return GraphSearchResult{}, nil
	}

	authorizedEdge := func(edge GraphEdge) ([]GraphEvidence, bool) {
		if len(allowedTypes) > 0 {
			if _, ok := allowedTypes[edge.RelationType]; !ok {
				return nil, false
			}
		}
		filtered := make([]GraphEvidence, 0, len(edge.Evidence))
		for _, evidence := range edge.Evidence {
			if len(allowedKnowledge) > 0 {
				if _, ok := allowedKnowledge[evidence.KnowledgeID]; !ok {
					continue
				}
			}
			if currentVersion, governed := currentVersions[evidence.KnowledgeID]; governed && evidence.KnowledgeVersionID != currentVersion {
				continue
			}
			filtered = append(filtered, evidence)
		}
		return filtered, len(filtered) > 0 || len(allowedKnowledge) == 0
	}
	type state struct {
		key       string
		path      []string
		pathEdges []GraphEdge
	}
	frontier := make([]state, 0, len(seedKeys))
	seedOrder := make([]string, 0, len(seedKeys))
	for key := range seedKeys {
		seedOrder = append(seedOrder, key)
	}
	sort.Strings(seedOrder)
	for _, key := range seedOrder {
		frontier = append(frontier, state{key: key, path: []string{key}})
	}
	result := GraphSearchResult{}
	seenNodes := make(map[string]bool)
	for _, node := range nodes {
		if _, ok := seedKeys[node.CanonicalKey]; ok {
			seenNodes[node.CanonicalKey] = true
			result.Nodes = append(result.Nodes, node)
		}
	}
	for depth := 0; depth < query.MaxDepth && len(frontier) > 0; depth++ {
		select {
		case <-ctx.Done():
			result.Truncated, result.TruncationReason = true, "timeout"
			return finalizeGraphResult(result, query), nil
		default:
		}
		if len(seenNodes) >= query.MaxExpandedNodes {
			result.Truncated, result.TruncationReason = true, "expanded_nodes"
			return finalizeGraphResult(result, query), nil
		}
		next := make([]state, 0)
		for _, current := range frontier {
			candidates := make([]state, 0)
			for _, edge := range edges {
				target, matches := graphEdgeTarget(edge, current.key, query.Direction)
				if !matches {
					continue
				}
				filteredEvidence, ok := authorizedEdge(edge)
				if !ok {
					continue
				}
				already := false
				for _, key := range current.path {
					if key == target {
						already = true
						break
					}
				}
				if already {
					continue
				}
				edge.Evidence = filteredEvidence
				candidates = append(candidates, state{key: target, path: append(append([]string(nil), current.path...), target), pathEdges: append(append([]GraphEdge(nil), current.pathEdges...), edge)})
			}
			sort.Slice(candidates, func(i, j int) bool { return pathStateScore(candidates[i]) > pathStateScore(candidates[j]) })
			if len(candidates) > query.BranchFactor {
				candidates = candidates[:query.BranchFactor]
				result.Truncated = true
				result.TruncationReason = "branch_factor"
			}
			for _, candidate := range candidates {
				if len(result.Paths) >= query.MaxPaths {
					result.Truncated, result.TruncationReason = true, "max_paths"
					return finalizeGraphResult(result, query), nil
				}
				if _, exists := seenNodes[candidate.key]; !exists {
					for _, node := range nodes {
						if node.CanonicalKey == candidate.key {
							result.Nodes = append(result.Nodes, node)
							break
						}
					}
					seenNodes[candidate.key] = true
				}
				next = append(next, candidate)
				if candidate.key != current.key {
					result.Paths = append(result.Paths, graphPathFromState(candidate))
				}
			}
		}
		frontier = next
	}
	return finalizeGraphResult(result, query), nil
}

func graphEdgeTarget(edge GraphEdge, current string, direction GraphDirection) (string, bool) {
	switch direction {
	case GraphDirectionOutgoing:
		return edge.Target, edge.Source == current
	case GraphDirectionIncoming:
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
func pathStateScore(s struct {
	key       string
	path      []string
	pathEdges []GraphEdge
}) float64 {
	return float64(len(s.pathEdges))
}
func graphPathFromState(s struct {
	key       string
	path      []string
	pathEdges []GraphEdge
}) GraphPath {
	path := GraphPath{NodeKeys: append([]string(nil), s.path...), Edges: append([]GraphEdge(nil), s.pathEdges...)}
	if len(path.Edges) == 0 {
		return path
	}
	total := 0.0
	seenEvidence := make(map[string]bool)
	for _, edge := range path.Edges {
		weight := edge.Weight
		if weight == 0 {
			weight = 0.5
		}
		total += weight
		for _, item := range edge.Evidence {
			key := item.KnowledgeID + ":" + item.ChunkID
			if !seenEvidence[key] {
				seenEvidence[key] = true
				path.Evidence = append(path.Evidence, item)
			}
		}
	}
	path.Score = total / float64(len(path.Edges)) / (1 + .25*float64(len(path.Edges)-1))
	return path
}
func finalizeGraphResult(result GraphSearchResult, query GraphQuery) GraphSearchResult {
	sort.SliceStable(result.Paths, func(i, j int) bool {
		if result.Paths[i].Score == result.Paths[j].Score {
			return strings.Join(result.Paths[i].NodeKeys, "\x00") < strings.Join(result.Paths[j].NodeKeys, "\x00")
		}
		return result.Paths[i].Score > result.Paths[j].Score
	})
	seen := map[string]bool{}
	paths := result.Paths[:0]
	for _, path := range result.Paths {
		key := strings.Join(path.NodeKeys, "\x00")
		if !seen[key] && path.Score >= query.MinPathScore {
			seen[key] = true
			paths = append(paths, path)
		}
	}
	result.Paths = paths
	edges := make(map[string]GraphEdge)
	for _, path := range result.Paths {
		result.Citations = append(result.Citations, path.Evidence...)
		for _, edge := range path.Edges {
			edges[edge.ID] = edge
		}
	}
	result.Edges = result.Edges[:0]
	for _, edge := range edges {
		result.Edges = append(result.Edges, edge)
	}
	sort.Slice(result.Edges, func(i, j int) bool { return result.Edges[i].ID < result.Edges[j].ID })
	return result
}
