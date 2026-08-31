package chatpipeline

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// PluginSearch implements search functionality for chat pipeline
type PluginSearchEntity struct {
	graphRepo      interfaces.RetrieveGraphRepository
	chunkRepo      interfaces.ChunkRepository
	knowledgeRepo  interfaces.KnowledgeRepository
	governanceRepo interfaces.KnowledgeGovernanceRepository
}

// NewPluginSearchEntity creates a new plugin search entity
func NewPluginSearchEntity(
	eventManager *EventManager,
	graphRepository interfaces.RetrieveGraphRepository,
	chunkRepository interfaces.ChunkRepository,
	knowledgeRepository interfaces.KnowledgeRepository,
	governanceRepository interfaces.KnowledgeGovernanceRepository,
) *PluginSearchEntity {
	res := &PluginSearchEntity{
		graphRepo:      graphRepository,
		chunkRepo:      chunkRepository,
		knowledgeRepo:  knowledgeRepository,
		governanceRepo: governanceRepository,
	}
	eventManager.Register(res)
	return res
}

// ActivationEvents returns the list of event types this plugin responds to
func (p *PluginSearchEntity) ActivationEvents() []types.EventType {
	return []types.EventType{types.ENTITY_SEARCH}
}

// OnEvent processes triggered events
func (p *PluginSearchEntity) OnEvent(ctx context.Context,
	eventType types.EventType, chatManage *types.ChatManage, next func() *PluginError,
) *PluginError {
	entity := chatManage.Entity
	if len(entity) == 0 {
		logger.Infof(ctx, "No entity found")
		return next()
	}

	// Use EntityKBIDs (knowledge bases with ExtractConfig enabled)
	knowledgeBaseIDs := chatManage.EntityKBIDs
	// Use EntityKnowledge (KnowledgeID -> KnowledgeBaseID mapping for graph-enabled files)
	entityKnowledge := chatManage.EntityKnowledge

	if len(knowledgeBaseIDs) == 0 && len(entityKnowledge) == 0 {
		logger.Warnf(ctx, "No knowledge base IDs or knowledge IDs with ExtractConfig enabled for entity search")
		return next()
	}

	// Parallel search across multiple knowledge bases and individual files
	var wg sync.WaitGroup
	var mu sync.Mutex
	var allNodes []*types.GraphNode
	var allRelations []*types.GraphRelation
	var allGraphResults []*types.GraphSearchResult

	// If specific KnowledgeIDs are provided, search by individual files
	if len(entityKnowledge) > 0 {
		logger.Infof(ctx, "Searching entities across %d knowledge file(s)", len(entityKnowledge))
		for knowledgeID, kbID := range entityKnowledge {
			wg.Add(1)
			go func(knowledgeBaseID, knowledgeID string) {
				defer wg.Done()

				graphData, graphResult, err := p.searchPaths(ctx, chatManage, knowledgeBaseID, []string{knowledgeID}, entity)
				if err != nil {
					logger.Errorf(ctx, "Failed to search entity in Knowledge %s: %v", knowledgeID, err)
					return
				}

				logger.Infof(
					ctx,
					"Knowledge %s entity search result count: %d nodes, %d relations",
					knowledgeID,
					len(graphData.Node),
					len(graphData.Relation),
				)

				mu.Lock()
				allNodes = append(allNodes, graphData.Node...)
				allRelations = append(allRelations, graphData.Relation...)
				allGraphResults = append(allGraphResults, graphResult)
				mu.Unlock()
			}(kbID, knowledgeID)
		}
	} else {
		// Otherwise, search by knowledge base
		logger.Infof(ctx, "Searching entities across %d knowledge base(s): %v", len(knowledgeBaseIDs), knowledgeBaseIDs)
		for _, kbID := range knowledgeBaseIDs {
			wg.Add(1)
			go func(knowledgeBaseID string) {
				defer wg.Done()

				graphData, graphResult, err := p.searchPaths(ctx, chatManage, knowledgeBaseID, nil, entity)
				if err != nil {
					logger.Errorf(ctx, "Failed to search entity in KB %s: %v", knowledgeBaseID, err)
					return
				}

				logger.Infof(
					ctx,
					"KB %s entity search result count: %d nodes, %d relations",
					knowledgeBaseID,
					len(graphData.Node),
					len(graphData.Relation),
				)

				mu.Lock()
				allNodes = append(allNodes, graphData.Node...)
				allRelations = append(allRelations, graphData.Relation...)
				allGraphResults = append(allGraphResults, graphResult)
				mu.Unlock()
			}(kbID)
		}
	}

	wg.Wait()

	// Merge graph data
	chatManage.GraphResult = &types.GraphData{
		Node:     allNodes,
		Relation: allRelations,
	}
	chatManage.GraphSearchResult = p.filterGovernedGraphResult(ctx, chatManage, mergeGraphSearchResults(allGraphResults))
	chatManage.GraphContext = types.RenderGraphContext(chatManage.GraphSearchResult, 12000)
	chatManage.GraphResult = graphSearchResultToGraphData(chatManage.GraphSearchResult)
	logger.Infof(ctx, "Total entity search result: %d nodes, %d relations", len(allNodes), len(allRelations))

	chunkIDs := filterSeenChunk(ctx, chatManage.GraphResult, chatManage.SearchResult)
	if len(chunkIDs) == 0 {
		logger.Infof(ctx, "No new chunk found")
		return next()
	}
	chunks, err := p.chunkRepo.ListChunksByID(ctx, types.MustTenantIDFromContext(ctx), chunkIDs)
	if err != nil {
		logger.Errorf(ctx, "Failed to list chunks, session_id: %s, error: %v", chatManage.SessionID, err)
		return next()
	}
	knowledgeIDs := []string{}
	for _, chunk := range chunks {
		knowledgeIDs = append(knowledgeIDs, chunk.KnowledgeID)
	}
	knowledges, err := p.knowledgeRepo.GetKnowledgeBatch(
		ctx,
		types.MustTenantIDFromContext(ctx),
		knowledgeIDs,
	)
	if err != nil {
		logger.Errorf(ctx, "Failed to list knowledge, session_id: %s, error: %v", chatManage.SessionID, err)
		return next()
	}

	knowledgeMap := map[string]*types.Knowledge{}
	for _, knowledge := range knowledges {
		knowledgeMap[knowledge.ID] = knowledge
	}
	var entityResults []*types.SearchResult
	for _, chunk := range chunks {
		knowledge := knowledgeMap[chunk.KnowledgeID]
		if knowledge == nil || !governedChunkVisible(knowledge, chunk) {
			continue
		}
		searchResult := chunk2SearchResult(chunk, knowledge)
		if knowledge.CurrentVersionID != "" && p.governanceRepo != nil {
			version, versionErr := p.governanceRepo.GetVersion(ctx, chatManage.TenantID, knowledge.CurrentVersionID)
			if versionErr != nil || version == nil || !version.IsRetrievable(time.Now().UTC()) {
				continue
			}
			searchResult.KnowledgeLayer = version.SourceMetadata.Layer
			searchResult.SourceCategory = version.SourceMetadata.SourceCategory
			searchResult.EffectiveAt = version.EffectiveAt
		}
		entityResults = append(entityResults, searchResult)
	}
	searchutil.EnrichSearchResultsImageInfo(ctx, p.chunkRepo, types.MustTenantIDFromContext(ctx), entityResults)
	chatManage.SearchResult = append(chatManage.SearchResult, entityResults...)
	// remove duplicate results
	chatManage.SearchResult = removeDuplicateResults(chatManage.SearchResult)
	if len(chatManage.SearchResult) == 0 {
		logger.Infof(ctx, "No new search result, session_id: %s", chatManage.SessionID)
		return ErrSearchNothing
	}
	logger.Infof(
		ctx,
		"search entity result count: %d, session_id: %s",
		len(chatManage.SearchResult),
		chatManage.SessionID,
	)
	return next()
}

// filterGovernedGraphResult removes evidence from graph paths before any
// GraphContext is rendered. Graph repositories select the active namespace,
// but the SQL knowledge version pointer remains the final authorization
// boundary for legacy and mixed-version graph data.
func (p *PluginSearchEntity) filterGovernedGraphResult(ctx context.Context, chatManage *types.ChatManage, result *types.GraphSearchResult) *types.GraphSearchResult {
	if result == nil || result.Fallback || p.knowledgeRepo == nil || chatManage == nil || chatManage.TenantID == 0 {
		return result
	}
	knowledgeIDs := make([]string, 0)
	seen := make(map[string]bool)
	collect := func(evidence []types.GraphEvidence) {
		for _, item := range evidence {
			if item.KnowledgeID != "" && !seen[item.KnowledgeID] {
				seen[item.KnowledgeID] = true
				knowledgeIDs = append(knowledgeIDs, item.KnowledgeID)
			}
		}
	}
	collect(result.Citations)
	for _, node := range result.Nodes {
		collect(node.Evidence)
	}
	for _, path := range result.Paths {
		collect(path.Evidence)
		for _, edge := range path.Edges {
			collect(edge.Evidence)
		}
	}
	for _, edge := range result.Edges {
		collect(edge.Evidence)
	}
	if len(knowledgeIDs) == 0 {
		return &types.GraphSearchResult{Fallback: true, FallbackReason: "graph_evidence_missing"}
	}
	knowledges, err := p.knowledgeRepo.GetKnowledgeBatch(ctx, chatManage.TenantID, knowledgeIDs)
	if err != nil {
		logger.Warnf(ctx, "Failed to load graph governance versions: %v", err)
		return &types.GraphSearchResult{Fallback: true, FallbackReason: "graph_governance_unavailable"}
	}
	byID := make(map[string]*types.Knowledge, len(knowledges))
	for _, knowledge := range knowledges {
		if knowledge != nil {
			byID[knowledge.ID] = knowledge
		}
	}
	versionVisible := make(map[string]bool)
	visible := func(item types.GraphEvidence) bool {
		knowledge := byID[item.KnowledgeID]
		if knowledge == nil {
			return false
		}
		if knowledge.CurrentVersionID == "" {
			return knowledge.PendingVersionID == "" && item.KnowledgeVersionID == ""
		}
		if item.KnowledgeVersionID != knowledge.CurrentVersionID {
			return false
		}
		if p.governanceRepo == nil {
			return true
		}
		if allowed, ok := versionVisible[knowledge.CurrentVersionID]; ok {
			return allowed
		}
		version, versionErr := p.governanceRepo.GetVersion(ctx, chatManage.TenantID, knowledge.CurrentVersionID)
		allowed := versionErr == nil && version != nil && version.IsRetrievable(time.Now().UTC())
		versionVisible[knowledge.CurrentVersionID] = allowed
		return allowed
	}
	filterEvidence := func(items []types.GraphEvidence) []types.GraphEvidence {
		filtered := make([]types.GraphEvidence, 0, len(items))
		for _, item := range items {
			if visible(item) {
				filtered = append(filtered, item)
			}
		}
		return filtered
	}
	filtered := *result
	filtered.Citations = filterEvidence(result.Citations)
	filtered.Edges = nil
	for _, edge := range result.Edges {
		edge.Evidence = filterEvidence(edge.Evidence)
		if len(edge.Evidence) > 0 {
			filtered.Edges = append(filtered.Edges, edge)
		}
	}
	filtered.Paths = nil
	for _, path := range result.Paths {
		path.Evidence = filterEvidence(path.Evidence)
		pathEdges := path.Edges
		path.Edges = nil
		for _, edge := range pathEdges {
			edge.Evidence = filterEvidence(edge.Evidence)
			if len(edge.Evidence) > 0 {
				path.Edges = append(path.Edges, edge)
			}
		}
		if len(path.Evidence) > 0 && len(path.Edges) > 0 {
			filtered.Paths = append(filtered.Paths, path)
		}
	}
	filtered.Nodes = nil
	usedNodes := make(map[string]bool)
	for _, path := range filtered.Paths {
		for _, key := range path.NodeKeys {
			usedNodes[key] = true
		}
	}
	for _, node := range result.Nodes {
		node.Evidence = filterEvidence(node.Evidence)
		if usedNodes[node.CanonicalKey] || len(node.Evidence) > 0 {
			filtered.Nodes = append(filtered.Nodes, node)
		}
	}
	types.EnsureGraphCitations(&filtered)
	return &filtered
}

func governedChunkVisible(knowledge *types.Knowledge, chunk *types.Chunk) bool {
	if knowledge == nil || chunk == nil {
		return false
	}
	if knowledge.CurrentVersionID == "" {
		return knowledge.PendingVersionID == "" && chunk.KnowledgeVersionID == ""
	}
	return chunk.KnowledgeVersionID == knowledge.CurrentVersionID
}

func (p *PluginSearchEntity) searchPaths(
	ctx context.Context,
	chatManage *types.ChatManage,
	knowledgeBaseID string,
	knowledgeIDs []string,
	seeds []string,
) (*types.GraphData, *types.GraphSearchResult, error) {
	if chatManage.TenantID == 0 {
		return &types.GraphData{}, &types.GraphSearchResult{
			Fallback:       true,
			FallbackReason: "tenant_scope_unavailable",
		}, nil
	}
	query := types.GraphQuery{
		Scope: types.GraphScope{
			TenantID:                 chatManage.TenantID,
			KnowledgeBaseID:          knowledgeBaseID,
			AllowedKnowledgeIDs:      knowledgeIDs,
			CurrentKnowledgeVersions: p.currentKnowledgeVersions(ctx, chatManage.TenantID, knowledgeBaseID, knowledgeIDs),
		},
		Seeds:            make([]types.GraphSeed, 0, len(seeds)),
		Direction:        types.GraphDirectionBoth,
		MaxDepth:         2,
		BranchFactor:     10,
		MaxExpandedNodes: 1000,
		MaxPaths:         100,
	}
	for _, seed := range seeds {
		query.Seeds = append(query.Seeds, types.GraphSeed{Name: seed})
	}
	result, err := p.graphRepo.SearchPaths(ctx, query)
	if err == nil && result != nil && !result.Fallback {
		types.FilterGraphSearchResult(result, query.Scope.CurrentKnowledgeVersions)
		types.EnsureGraphCitations(result)
		return graphSearchResultToGraphData(result), result, nil
	}
	// Legacy one-hop nodes do not carry relationship-level evidence or an
	// authorized knowledge scope. Do not convert them into typed graph data;
	// the caller will continue with ordinary text retrieval instead.
	if result == nil {
		reason := "no_graph_result"
		if err != nil {
			reason = "graph_query_failed"
		}
		result = &types.GraphSearchResult{Fallback: true, FallbackReason: reason}
	}
	result.Fallback = true
	if result.FallbackReason == "" && err != nil {
		result.FallbackReason = "graph_query_failed"
	}
	return &types.GraphData{}, result, nil
}

func (p *PluginSearchEntity) currentKnowledgeVersions(ctx context.Context, tenantID uint64, knowledgeBaseID string, knowledgeIDs []string) map[string]string {
	if p.knowledgeRepo == nil {
		return nil
	}
	var knowledges []*types.Knowledge
	var err error
	if len(knowledgeIDs) > 0 {
		knowledges, err = p.knowledgeRepo.GetKnowledgeBatch(ctx, tenantID, knowledgeIDs)
	} else {
		knowledges, err = p.knowledgeRepo.ListKnowledgeByKnowledgeBaseID(ctx, tenantID, knowledgeBaseID)
	}
	if err != nil {
		logger.Warnf(ctx, "Failed to resolve current graph versions: %v", err)
		return nil
	}
	versions := make(map[string]string)
	for _, knowledge := range knowledges {
		if knowledge != nil && knowledge.CurrentVersionID != "" {
			versions[knowledge.ID] = knowledge.CurrentVersionID
		}
	}
	return versions
}

func graphSearchResultToGraphData(result *types.GraphSearchResult) *types.GraphData {
	graph := &types.GraphData{}
	if result == nil {
		return graph
	}
	for _, node := range result.Nodes {
		graph.Node = append(graph.Node, &types.GraphNode{Name: node.Name, Chunks: evidenceChunkIDs(node.Evidence)})
	}
	for _, edge := range result.Edges {
		graph.Relation = append(graph.Relation, &types.GraphRelation{Node1: edge.Source, Node2: edge.Target, Type: edge.RelationType})
	}
	for _, path := range result.Paths {
		for _, evidence := range path.Evidence {
			if evidence.ChunkID == "" {
				continue
			}
			for _, node := range graph.Node {
				if !hasChunkID(node.Chunks, evidence.ChunkID) {
					node.Chunks = append(node.Chunks, evidence.ChunkID)
				}
			}
		}
	}
	return graph
}

func evidenceChunkIDs(evidence []types.GraphEvidence) []string {
	ids := make([]string, 0, len(evidence))
	for _, item := range evidence {
		if item.ChunkID != "" && !hasChunkID(ids, item.ChunkID) {
			ids = append(ids, item.ChunkID)
		}
	}
	return ids
}

func hasChunkID(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func mergeGraphSearchResults(results []*types.GraphSearchResult) *types.GraphSearchResult {
	if len(results) == 0 {
		return nil
	}
	merged := &types.GraphSearchResult{}
	nodes, edges, paths := map[string]types.CanonicalEntity{}, map[string]types.GraphEdge{}, map[string]types.GraphPath{}
	citations := map[string]types.GraphEvidence{}
	addCitation := func(citation types.GraphEvidence) {
		key := citation.KnowledgeID + "\x00" + citation.ChunkID
		if _, exists := citations[key]; !exists {
			citations[key] = citation
		}
	}
	for _, result := range results {
		if result == nil {
			continue
		}
		merged.Truncated = merged.Truncated || result.Truncated
		merged.Fallback = merged.Fallback || result.Fallback
		if merged.TruncationReason == "" {
			merged.TruncationReason = result.TruncationReason
		}
		if merged.FallbackReason == "" {
			merged.FallbackReason = result.FallbackReason
		}
		for _, node := range result.Nodes {
			nodes[node.CanonicalKey] = node
		}
		for _, edge := range result.Edges {
			edges[edge.ID] = edge
		}
		for _, path := range result.Paths {
			paths[strings.Join(path.NodeKeys, "\x00")] = path
		}
		for _, citation := range result.Citations {
			addCitation(citation)
		}
	}
	for _, node := range nodes {
		merged.Nodes = append(merged.Nodes, node)
	}
	for _, edge := range edges {
		merged.Edges = append(merged.Edges, edge)
	}
	for _, path := range paths {
		merged.Paths = append(merged.Paths, path)
		for _, citation := range path.Evidence {
			addCitation(citation)
		}
	}
	for _, citation := range citations {
		merged.Citations = append(merged.Citations, citation)
	}
	sort.Slice(merged.Nodes, func(i, j int) bool { return merged.Nodes[i].CanonicalKey < merged.Nodes[j].CanonicalKey })
	sort.Slice(merged.Edges, func(i, j int) bool { return merged.Edges[i].ID < merged.Edges[j].ID })
	sort.Slice(merged.Paths, func(i, j int) bool {
		if merged.Paths[i].Score == merged.Paths[j].Score {
			return strings.Join(merged.Paths[i].NodeKeys, "\x00") < strings.Join(merged.Paths[j].NodeKeys, "\x00")
		}
		return merged.Paths[i].Score > merged.Paths[j].Score
	})
	sort.Slice(merged.Citations, func(i, j int) bool {
		left := merged.Citations[i].KnowledgeID + "\x00" + merged.Citations[i].ChunkID
		right := merged.Citations[j].KnowledgeID + "\x00" + merged.Citations[j].ChunkID
		return left < right
	})
	return merged
}

// filterSeenChunk filters seen chunks from the graph
func filterSeenChunk(ctx context.Context, graph *types.GraphData, searchResult []*types.SearchResult) []string {
	seen := map[string]bool{}
	for _, chunk := range searchResult {
		seen[chunk.ID] = true
	}
	logger.Infof(ctx, "filterSeenChunk: seen count: %d", len(seen))

	chunkIDs := []string{}
	for _, node := range graph.Node {
		for _, chunkID := range node.Chunks {
			if seen[chunkID] {
				continue
			}
			seen[chunkID] = true
			chunkIDs = append(chunkIDs, chunkID)
		}
	}
	logger.Infof(ctx, "filterSeenChunk: new chunkIDs count: %d", len(chunkIDs))
	return chunkIDs
}

// chunk2SearchResult converts a chunk to a search result
func graphSearchKnowledgeMetadata(knowledge *types.Knowledge, chunk *types.Chunk) map[string]string {
	metadata := knowledge.GetMetadata()
	if strings.TrimSpace(knowledge.PendingVersionID) != "" && chunk.KnowledgeVersionID != strings.TrimSpace(knowledge.PendingVersionID) {
		delete(metadata, "content")
		delete(metadata, "status")
		delete(metadata, "version")
		delete(metadata, "updated_at")
	}
	return metadata
}

func graphSearchKnowledgeDisplay(knowledge *types.Knowledge, chunk *types.Chunk) (string, string, string, string) {
	if strings.TrimSpace(knowledge.PendingVersionID) != "" && chunk.KnowledgeVersionID != strings.TrimSpace(knowledge.PendingVersionID) {
		return "", "", "", ""
	}
	return knowledge.Title, knowledge.FileName, knowledge.Source, knowledge.Description
}

func chunk2SearchResult(chunk *types.Chunk, knowledge *types.Knowledge) *types.SearchResult {
	title, filename, source, description := graphSearchKnowledgeDisplay(knowledge, chunk)
	return &types.SearchResult{
		ID:                   chunk.ID,
		Content:              chunk.Content,
		KnowledgeID:          chunk.KnowledgeID,
		KnowledgeVersionID:   chunk.KnowledgeVersionID,
		ChunkIndex:           chunk.ChunkIndex,
		KnowledgeTitle:       title,
		StartAt:              chunk.StartAt,
		EndAt:                chunk.EndAt,
		Seq:                  chunk.ChunkIndex,
		Score:                1.0,
		MatchType:            types.MatchTypeGraph,
		Metadata:             graphSearchKnowledgeMetadata(knowledge, chunk),
		ChunkType:            string(chunk.ChunkType),
		ParentChunkID:        chunk.ParentChunkID,
		ImageInfo:            chunk.ImageInfo,
		KnowledgeFilename:    filename,
		KnowledgeSource:      source,
		KnowledgeChannel:     knowledge.Channel,
		KnowledgeDescription: description,
		ChunkMetadata:        chunk.Metadata,
		KnowledgeBaseID:      knowledge.KnowledgeBaseID,
	}
}
