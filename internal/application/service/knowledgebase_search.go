package service

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
)

// GetQueryEmbedding computes the query embedding using the embedding model
// associated with the given knowledge base. Callers can pre-compute and reuse
// the result across multiple KBs that share the same embedding model to avoid
// redundant embedding API calls.
func (s *knowledgeBaseService) GetQueryEmbedding(ctx context.Context, kbID string, queryText string) ([]float32, error) {
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, err
	}

	currentTenantID := types.MustTenantIDFromContext(ctx)
	var embeddingModel embedding.Embedder

	if kb.TenantID != currentTenantID {
		embeddingModel, err = s.modelService.GetEmbeddingModelForTenant(ctx, kb.EmbeddingModelID, kb.TenantID)
	} else {
		embeddingModel, err = s.modelService.GetEmbeddingModel(ctx, kb.EmbeddingModelID)
	}
	if err != nil {
		logger.Errorf(ctx, "GetQueryEmbedding: failed to get embedding model %s: %v", kb.EmbeddingModelID, err)
		return nil, err
	}

	return embeddingModel.Embed(ctx, queryText)
}

// ResolveEmbeddingModelKeys resolves embedding model IDs to their actual model
// identity key (name + endpoint). KBs using the same underlying model across
// different tenants will share the same key, enabling optimal grouping.
func (s *knowledgeBaseService) ResolveEmbeddingModelKeys(ctx context.Context, kbs []*types.KnowledgeBase) map[string]string {
	type modelRef struct {
		ModelID  string
		TenantID uint64
	}

	// Deduplicate model references
	uniqueRefs := make(map[modelRef]struct{})
	kbRefs := make(map[string]modelRef, len(kbs))
	for _, kb := range kbs {
		ref := modelRef{ModelID: kb.EmbeddingModelID, TenantID: kb.TenantID}
		uniqueRefs[ref] = struct{}{}
		kbRefs[kb.ID] = ref
	}

	// Resolve each unique (modelID, tenantID) to a model identity key
	resolvedKeys := make(map[modelRef]string, len(uniqueRefs))
	for ref := range uniqueRefs {
		tenantCtx := context.WithValue(ctx, types.TenantIDContextKey, ref.TenantID)
		model, err := s.modelService.GetModelByID(tenantCtx, ref.ModelID)
		if err != nil || model == nil {
			logger.Warnf(ctx, "ResolveEmbeddingModelKeys: cannot resolve model %s for tenant %d: %v", ref.ModelID, ref.TenantID, err)
			resolvedKeys[ref] = ref.ModelID
			continue
		}
		resolvedKeys[ref] = model.Name + "|" + model.Parameters.BaseURL
	}

	result := make(map[string]string, len(kbs))
	for _, kb := range kbs {
		result[kb.ID] = resolvedKeys[kbRefs[kb.ID]]
	}
	return result
}

// HybridSearch performs hybrid search, including vector retrieval and keyword retrieval.
//
// id is the "primary" knowledge base ID used to resolve the embedding model and
// determine the KB type (e.g. FAQ). When params.KnowledgeBaseIDs is set, those
// IDs are used for the actual retrieval scope instead of id alone, allowing a
// single call to span multiple KBs that share the same embedding model. In that
// case id should be any one of those KBs (typically the first) so that its
// embedding model and type configuration are used for the search.
func (s *knowledgeBaseService) HybridSearch(ctx context.Context,
	id string,
	params types.SearchParams,
) ([]*types.SearchResult, error) {
	// Determine the set of KB IDs to search
	searchKBIDs := params.KnowledgeBaseIDs
	if len(searchKBIDs) == 0 {
		searchKBIDs = []string{id}
	}
	requested := make(map[string]struct{}, len(searchKBIDs))
	for _, kbID := range searchKBIDs {
		requested[kbID] = struct{}{}
	}
	visibleKBs, err := s.GetKnowledgeBasesByIDsOnly(ctx, searchKBIDs)
	if err != nil {
		return nil, err
	}
	for _, kb := range visibleKBs {
		delete(requested, kb.ID)
	}
	if len(requested) > 0 {
		return nil, ErrKnowledgeBaseAccessDenied
	}

	logger.Infof(ctx, "Hybrid search parameters, knowledge base IDs: %v, query text: %s", searchKBIDs, params.QueryText)

	tenantInfo, _ := types.TenantInfoFromContext(ctx)

	// Create a composite retrieval engine with tenant's configured retrievers
	retrieveEngine, err := retriever.NewCompositeRetrieveEngine(s.retrieveEngine, tenantInfo.GetEffectiveEngines())
	if err != nil {
		logger.Errorf(ctx, "Failed to create retrieval engine: %v", err)
		return nil, err
	}

	kb, err := s.repo.GetKnowledgeBaseByID(ctx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": id,
		})
		return nil, err
	}

	// Preserve the legacy over-retrieval behavior when callers do not provide
	// explicit per-channel budgets.
	vectorMatchCount, keywordMatchCount := resolveRetrievalMatchCounts(params)
	logger.Infof(ctx,
		"Hybrid retrieval budgets: vector=%d, keyword=%d, fusion=%d, rrf_vector_weight=%.2f",
		vectorMatchCount, keywordMatchCount, params.MatchCount, params.RRFVectorWeight,
	)

	// Build retrieval parameters for vector and keyword engines
	var vectorKnowledgeIDs []string
	if retrieveEngine.SupportRetriever(types.VectorRetrieverType) && !params.DisableVectorMatch && shouldUseVectorRetrieval(kb) {
		vectorKnowledgeIDs, err = s.compatibleVectorKnowledgeIDs(ctx, kb, visibleKBs, params.KnowledgeIDs)
		if err != nil {
			logger.Warnf(ctx, "Skipping vector retrieval because embedding compatibility could not be resolved: %v", err)
		}
	}
	retrieveParams, err := s.buildRetrievalParams(
		ctx, retrieveEngine, kb, params, searchKBIDs, vectorKnowledgeIDs,
		vectorMatchCount, keywordMatchCount,
	)
	if err != nil {
		return nil, err
	}
	if len(retrieveParams) == 0 {
		// No retrievable pipelines for this KB (e.g. a wiki-only or graph-only
		// KB that has neither vector nor keyword indexing). Return empty
		// results rather than erroring so callers that combine multiple KB
		// scopes (agent knowledge_search tool, chat pipeline, etc.) degrade
		// gracefully when one of the scopes is non-searchable.
		logger.Infof(ctx, "No retrievable indexing pipelines for KB %s (vector=%v, keyword=%v), returning empty results",
			kb.ID, kb.IsVectorEnabled(), kb.IsKeywordEnabled())
		return nil, nil
	}

	// Execute retrieval using the configured engines
	logger.Infof(ctx, "Starting retrieval, parameter count: %d", len(retrieveParams))
	retrieveResults, err := retrieveEngine.Retrieve(ctx, retrieveParams)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_ids": searchKBIDs,
			"query_text":         params.QueryText,
		})
		return nil, err
	}

	// Separate and fuse retrieval results
	vectorResults, keywordResults := classifyRetrievalResults(ctx, retrieveResults)
	if len(vectorResults) == 0 && len(keywordResults) == 0 {
		logger.Info(ctx, "No search results found")
		return nil, nil
	}
	logger.Infof(ctx, "Result count before fusion: vector=%d, keyword=%d", len(vectorResults), len(keywordResults))

	deduplicatedChunks := fuseOrDeduplicate(ctx, vectorResults, keywordResults, params.RRFVectorWeight)
	if len(vectorResults) > 0 && len(keywordResults) > 0 && params.RerankCandidateCount > 0 {
		deduplicatedChunks = preserveRetrieverLeaders(
			deduplicatedChunks,
			vectorResults,
			keywordResults,
			params.RerankCandidateCount,
			params.MatchCount,
		)
	}

	kb.EnsureDefaults()

	// FAQ-specific post-processing: iterative retrieval or negative question filtering
	deduplicatedChunks = s.applyFAQPostProcessing(
		ctx, kb, deduplicatedChunks, vectorResults, retrieveEngine, retrieveParams, params, vectorMatchCount,
	)

	// Limit to MatchCount
	if len(deduplicatedChunks) > params.MatchCount {
		deduplicatedChunks = deduplicatedChunks[:params.MatchCount]
	}

	results, err := s.processSearchResults(ctx, deduplicatedChunks, params.SkipContextEnrichment)
	if err != nil {
		return nil, err
	}
	markKeywordLeader(results, keywordResults)
	return results, nil
}

func markKeywordLeader(results []*types.SearchResult, keywordResults []*types.IndexWithScore) {
	if len(keywordResults) == 0 {
		return
	}
	leaderID := keywordResults[0].ChunkID
	for _, result := range results {
		if result.ID != leaderID {
			continue
		}
		if result.Metadata == nil {
			result.Metadata = make(map[string]string)
		}
		result.Metadata["keyword_leader"] = "true"
		return
	}
}

func resolveRetrievalMatchCounts(params types.SearchParams) (int, int) {
	defaultRecallCount := max(params.MatchCount*5, 50)
	vectorMatchCount := params.VectorMatchCount
	if vectorMatchCount <= 0 {
		vectorMatchCount = defaultRecallCount
	}
	keywordMatchCount := params.KeywordMatchCount
	if keywordMatchCount <= 0 {
		keywordMatchCount = defaultRecallCount
	}
	return min(vectorMatchCount, 500), min(keywordMatchCount, 500)
}

// buildRetrievalParams constructs the vector and keyword retrieval parameters
// based on the knowledge base type, engine capabilities, and search params.
func (s *knowledgeBaseService) buildRetrievalParams(
	ctx context.Context,
	retrieveEngine *retriever.CompositeRetrieveEngine,
	kb *types.KnowledgeBase,
	params types.SearchParams,
	searchKBIDs []string,
	vectorKnowledgeIDs []string,
	vectorMatchCount, keywordMatchCount int,
) ([]types.RetrieveParams, error) {
	currentTenantID := types.MustTenantIDFromContext(ctx)
	var retrieveParams []types.RetrieveParams

	// The platform default supplies a missing embedding model ID. Only the
	// indexing strategy decides whether this knowledge base has vector data.
	vectorIndexed := shouldUseVectorRetrieval(kb)

	// Add vector retrieval params if supported
	if retrieveEngine.SupportRetriever(types.VectorRetrieverType) && !params.DisableVectorMatch && vectorIndexed && vectorKnowledgeIDs != nil {
		logger.Info(ctx, "Vector retrieval supported, preparing vector retrieval parameters")

		var queryEmbedding []float32

		if len(params.QueryEmbedding) > 0 {
			queryEmbedding = params.QueryEmbedding
			logger.Infof(ctx, "Using pre-computed query embedding, vector length: %d", len(queryEmbedding))
		} else {
			logger.Infof(ctx, "Getting embedding model, model ID: %s", kb.EmbeddingModelID)

			// Check if this is a cross-tenant shared knowledge base
			// For shared KB, we must use the source tenant's embedding model to ensure vector compatibility
			var embeddingModel embedding.Embedder
			var err error
			if kb.TenantID != currentTenantID {
				logger.Infof(ctx, "Cross-tenant knowledge base detected, using source tenant's embedding model. KB tenant: %d, current tenant: %d", kb.TenantID, currentTenantID)
				embeddingModel, err = s.modelService.GetEmbeddingModelForTenant(ctx, kb.EmbeddingModelID, kb.TenantID)
			} else {
				embeddingModel, err = s.modelService.GetEmbeddingModel(ctx, kb.EmbeddingModelID)
			}

			if err != nil {
				logger.Errorf(ctx, "Failed to get embedding model, model ID: %s, error: %v", kb.EmbeddingModelID, err)
				return nil, err
			}
			logger.Infof(ctx, "Embedding model retrieved: %v", embeddingModel)

			logger.Info(ctx, "Starting to generate query embedding")
			queryEmbedding, err = embeddingModel.Embed(ctx, params.QueryText)
			if err != nil {
				logger.Errorf(ctx, "Failed to embed query text, query text: %s, error: %v", params.QueryText, err)
				return nil, err
			}
			logger.Infof(ctx, "Query embedding generated successfully, embedding vector length: %d", len(queryEmbedding))
		}

		vectorParams := types.RetrieveParams{
			Query:            params.QueryText,
			Embedding:        queryEmbedding,
			KnowledgeBaseIDs: searchKBIDs,
			TopK:             vectorMatchCount,
			Threshold:        params.VectorThreshold,
			RetrieverType:    types.VectorRetrieverType,
			KnowledgeIDs:     vectorKnowledgeIDs,
			TagIDs:           params.TagIDs,
		}

		// For FAQ knowledge base, use FAQ index
		if kb.Type == types.KnowledgeBaseTypeFAQ {
			vectorParams.KnowledgeType = types.KnowledgeTypeFAQ
		}

		retrieveParams = append(retrieveParams, vectorParams)
		logger.Info(ctx, "Vector retrieval parameters setup completed")
	}

	// Add keyword retrieval params if supported, KB has keyword indexing, and not FAQ
	if retrieveEngine.SupportRetriever(types.KeywordsRetrieverType) && !params.DisableKeywordsMatch &&
		kb.IsKeywordEnabled() && kb.Type != types.KnowledgeBaseTypeFAQ {
		logger.Info(ctx, "Keyword retrieval supported, preparing keyword retrieval parameters")
		retrieveParams = append(retrieveParams, types.RetrieveParams{
			Query:            params.QueryText,
			KnowledgeBaseIDs: searchKBIDs,
			TopK:             keywordMatchCount,
			Threshold:        params.KeywordThreshold,
			RetrieverType:    types.KeywordsRetrieverType,
			KnowledgeIDs:     params.KnowledgeIDs,
			TagIDs:           params.TagIDs,
		})
		logger.Info(ctx, "Keyword retrieval parameters setup completed")
	}

	return retrieveParams, nil
}

func (s *knowledgeBaseService) compatibleVectorKnowledgeIDs(
	ctx context.Context,
	kb *types.KnowledgeBase,
	visibleKBs []*types.KnowledgeBase,
	requestedKnowledgeIDs []string,
) ([]string, error) {
	activeModel, err := s.modelService.GetModelByID(ctx, kb.EmbeddingModelID)
	if err != nil {
		return nil, err
	}
	models, err := s.modelService.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	knowledges := make([]*types.Knowledge, 0)
	for _, visibleKB := range visibleKBs {
		items, listErr := s.kgRepo.ListKnowledgeByKnowledgeBaseID(ctx, visibleKB.TenantID, visibleKB.ID)
		if listErr != nil {
			return nil, listErr
		}
		knowledges = append(knowledges, items...)
	}
	return compatibleKnowledgeIDs(activeModel, models, knowledges, requestedKnowledgeIDs), nil
}

func compatibleKnowledgeIDs(
	activeModel *types.Model,
	models []*types.Model,
	knowledges []*types.Knowledge,
	requestedKnowledgeIDs []string,
) []string {
	modelsByID := make(map[string]*types.Model, len(models))
	for _, model := range models {
		if model != nil {
			modelsByID[model.ID] = model
		}
	}
	requested := make(map[string]struct{}, len(requestedKnowledgeIDs))
	for _, id := range requestedKnowledgeIDs {
		requested[id] = struct{}{}
	}
	compatible := make([]string, 0, len(knowledges))
	for _, knowledge := range knowledges {
		if knowledge == nil {
			continue
		}
		if len(requested) > 0 {
			if _, ok := requested[knowledge.ID]; !ok {
				continue
			}
		}
		if knowledgeEmbeddingCompatible(activeModel, knowledge, modelsByID[knowledge.EmbeddingModelID]) {
			compatible = append(compatible, knowledge.ID)
		}
	}
	if len(compatible) == 0 {
		return nil
	}
	return compatible
}

func knowledgeEmbeddingCompatible(activeModel *types.Model, knowledge *types.Knowledge, legacyModel *types.Model) bool {
	if activeModel == nil || knowledge == nil {
		return false
	}
	if knowledge.EmbeddingDimension > 0 {
		activeParams := activeModel.Parameters.EmbeddingParameters
		if activeParams.Dimension != knowledge.EmbeddingDimension {
			return false
		}
		activeID := strings.TrimSpace(activeParams.CompatibilityID)
		knowledgeID := strings.TrimSpace(knowledge.EmbeddingCompatibilityID)
		if activeID != "" || knowledgeID != "" {
			return activeID != "" && activeID == knowledgeID
		}
		return activeModel.ID == knowledge.EmbeddingModelID
	}
	return embeddingModelsCompatible(activeModel, legacyModel)
}

func embeddingModelsCompatible(left, right *types.Model) bool {
	if left == nil || right == nil {
		return false
	}
	leftParams := left.Parameters.EmbeddingParameters
	rightParams := right.Parameters.EmbeddingParameters
	if leftParams.Dimension <= 0 || rightParams.Dimension <= 0 || leftParams.Dimension != rightParams.Dimension {
		return false
	}
	leftID := strings.TrimSpace(leftParams.CompatibilityID)
	rightID := strings.TrimSpace(rightParams.CompatibilityID)
	if leftID == "" || rightID == "" {
		return left.ID == right.ID
	}
	return leftID == rightID
}

func shouldUseVectorRetrieval(kb *types.KnowledgeBase) bool {
	return kb != nil && kb.IsVectorEnabled()
}
