package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// supersededVersionPurgeDeps is the narrow dependency surface for removing a
// superseded knowledge version from retrieval indexes and Postgres chunks.
type supersededVersionPurgeDeps struct {
	chunkService  interfaces.ChunkService
	deleteIndexes func(ctx context.Context, chunkIDs []string, dimension int, knowledgeType string) error
	knowledgeType string
	dimension     int
}

func purgeSupersededVersionIndexes(
	ctx context.Context,
	deps supersededVersionPurgeDeps,
	knowledgeID string,
	supersededVersionID string,
) error {
	knowledgeID = strings.TrimSpace(knowledgeID)
	supersededVersionID = strings.TrimSpace(supersededVersionID)
	if knowledgeID == "" || supersededVersionID == "" {
		return nil
	}
	if deps.chunkService == nil {
		return nil
	}
	chunks, err := deps.chunkService.ListChunksByKnowledgeID(ctx, knowledgeID)
	if err != nil {
		return fmt.Errorf("list chunks for superseded version purge: %w", err)
	}
	indexChunkIDs := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk == nil || strings.TrimSpace(chunk.KnowledgeVersionID) != supersededVersionID {
			continue
		}
		if chunk.ID == "" {
			continue
		}
		// Parent/entity/relationship/summary chunks are not indexed into RAG.
		if chunk.ChunkType == types.ChunkTypeParentText ||
			chunk.ChunkType == types.ChunkTypeEntity ||
			chunk.ChunkType == types.ChunkTypeRelationship ||
			chunk.ChunkType == types.ChunkTypeSummary {
			continue
		}
		indexChunkIDs = append(indexChunkIDs, chunk.ID)
	}
	if deps.deleteIndexes != nil && len(indexChunkIDs) > 0 {
		if err := deps.deleteIndexes(ctx, indexChunkIDs, deps.dimension, deps.knowledgeType); err != nil {
			return fmt.Errorf("delete superseded version indexes: %w", err)
		}
	}
	n, err := deps.chunkService.DeleteChunksByKnowledgeVersionID(ctx, knowledgeID, supersededVersionID)
	if err != nil {
		return fmt.Errorf("delete superseded version chunks: %w", err)
	}
	logger.Infof(ctx, "[KnowledgePublish] purged superseded version knowledge=%s version=%s index=%d db=%d",
		knowledgeID, supersededVersionID, len(indexChunkIDs), n)
	return nil
}

// staleChunkIDsForDeletion returns chunk IDs that are neither the active
// published version nor an in-review pending version.
func staleChunkIDsForDeletion(chunks []*types.Chunk, currentVersionID, pendingVersionID string) []string {
	currentVersionID = strings.TrimSpace(currentVersionID)
	pendingVersionID = strings.TrimSpace(pendingVersionID)
	ids := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk == nil || chunk.ID == "" {
			continue
		}
		versionID := strings.TrimSpace(chunk.KnowledgeVersionID)
		if currentVersionID != "" && versionID == currentVersionID {
			continue
		}
		if pendingVersionID != "" && versionID == pendingVersionID {
			continue
		}
		ids = append(ids, chunk.ID)
	}
	return ids
}

func indexableChunksForVersion(chunks []*types.Chunk, versionID, title string) []*types.IndexInfo {
	versionID = strings.TrimSpace(versionID)
	titlePrefix := ""
	if trimmed := strings.TrimSpace(title); trimmed != "" {
		titlePrefix = trimmed + "\n"
	}
	indexInfo := make([]*types.IndexInfo, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk == nil {
			continue
		}
		if versionID != "" && strings.TrimSpace(chunk.KnowledgeVersionID) != versionID {
			continue
		}
		if chunk.ChunkType == types.ChunkTypeParentText ||
			chunk.ChunkType == types.ChunkTypeEntity ||
			chunk.ChunkType == types.ChunkTypeRelationship ||
			chunk.ChunkType == types.ChunkTypeSummary {
			continue
		}
		indexInfo = append(indexInfo, documentChunkIndexInfo(chunk, titlePrefix+chunk.Content, chunk.ID))
		meta, metaErr := chunk.DocumentMetadata()
		if metaErr != nil || meta == nil {
			continue
		}
		for _, question := range meta.GeneratedQuestions {
			entry := documentChunkIndexInfo(chunk, question.Question, fmt.Sprintf("%s-%s", chunk.ID, question.ID))
			entry.IsGeneratedQuestion = true
			indexInfo = append(indexInfo, entry)
		}
	}
	return indexInfo
}

type versionIndexPurger struct {
	chunkService   interfaces.ChunkService
	kbService      interfaces.KnowledgeBaseService
	knowledgeRepo  interfaces.KnowledgeRepository
	tenantService  interfaces.TenantService
	modelService   interfaces.ModelService
	retrieveEngine interfaces.RetrieveEngineRegistry
}

// PurgeAfterActivation drops every retrieval hit for the knowledge document,
// rewrites indexes for the newly active version only, then deletes superseded
// Postgres chunks (pending-review versions are kept until they publish or drop).
func (p *versionIndexPurger) PurgeAfterActivation(ctx context.Context, tenantID uint64, knowledgeID, supersededVersionID string) error {
	if strings.TrimSpace(supersededVersionID) == "" {
		return nil
	}
	return p.reconcileCurrentVersionIndexes(ctx, tenantID, knowledgeID, supersededVersionID)
}

// ReconcileCurrentVersionIndexes is the operator/backfill entry point used to
// repair indexes and delete superseded DB chunks that are neither current nor pending.
func (p *versionIndexPurger) ReconcileCurrentVersionIndexes(ctx context.Context, tenantID uint64, knowledgeID string) error {
	return p.reconcileCurrentVersionIndexes(ctx, tenantID, knowledgeID, "reconcile")
}

func (p *versionIndexPurger) reconcileCurrentVersionIndexes(ctx context.Context, tenantID uint64, knowledgeID, reason string) error {
	if p == nil || p.chunkService == nil {
		return nil
	}
	// Model lookups require TenantID in context (CLI / background callers may omit it).
	if _, ok := types.TenantIDFromContext(ctx); !ok {
		ctx = context.WithValue(ctx, types.TenantIDContextKey, tenantID)
	}
	knowledge, err := p.knowledgeRepo.GetKnowledgeByID(ctx, tenantID, knowledgeID)
	if err != nil {
		return fmt.Errorf("load knowledge for index reconcile: %w", err)
	}
	if knowledge == nil {
		return nil
	}
	kb, err := p.kbService.GetKnowledgeBaseByID(ctx, knowledge.KnowledgeBaseID)
	if err != nil {
		return fmt.Errorf("load knowledge base for index reconcile: %w", err)
	}
	chunks, err := p.chunkService.ListIndexableChunksByKnowledgeID(ctx, knowledge.ID)
	if err != nil {
		return fmt.Errorf("list chunks for index reconcile: %w", err)
	}
	currentVersionID := strings.TrimSpace(knowledge.CurrentVersionID)
	pendingVersionID := strings.TrimSpace(knowledge.PendingVersionID)

	if kb != nil && (kb.IsVectorEnabled() || kb.IsKeywordEnabled()) {
		tenant, err := p.tenantService.GetTenantByID(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("load tenant for index reconcile: %w", err)
		}
		if tenant == nil {
			return fmt.Errorf("tenant %d not found for index reconcile", tenantID)
		}
		engine, err := retriever.NewCompositeRetrieveEngine(p.retrieveEngine, tenant.GetEffectiveEngines())
		if err != nil {
			return fmt.Errorf("init retrieve engine for index reconcile: %w", err)
		}
		var embedder embedding.Embedder
		dimension := 0
		if kb.IsVectorEnabled() && strings.TrimSpace(knowledge.EmbeddingModelID) != "" && p.modelService != nil {
			embedder, err = p.modelService.GetEmbeddingModel(ctx, knowledge.EmbeddingModelID)
			if err != nil {
				return fmt.Errorf("get embedding model for index reconcile: %w", err)
			}
			if embedder != nil {
				dimension = embedder.GetDimensions()
			}
		}
		if err := engine.DeleteByKnowledgeIDList(ctx, []string{knowledge.ID}, dimension, kb.Type); err != nil {
			return fmt.Errorf("clear knowledge indexes: %w", err)
		}
		indexInfo := indexableChunksForVersion(chunks, currentVersionID, knowledge.Title)
		if len(indexInfo) == 0 {
			logger.Infof(ctx, "[KnowledgePublish] cleared indexes knowledge=%s reason=%s current=%s (no indexable chunks)",
				knowledge.ID, reason, currentVersionID)
		} else if err := engine.BatchIndex(ctx, embedder, indexInfo); err != nil {
			return fmt.Errorf("reindex active version: %w", err)
		} else {
			logger.Infof(ctx, "[KnowledgePublish] reindexed knowledge=%s reason=%s current=%s indexed=%d",
				knowledge.ID, reason, currentVersionID, len(indexInfo))
		}
	}

	keepVersions := make([]string, 0, 2)
	if currentVersionID != "" {
		keepVersions = append(keepVersions, currentVersionID)
	}
	if pendingVersionID != "" {
		keepVersions = append(keepVersions, pendingVersionID)
	}
	deletedDB, err := p.chunkService.DeleteChunksExcludingVersions(ctx, knowledge.ID, keepVersions)
	if err != nil {
		return fmt.Errorf("delete superseded db chunks: %w", err)
	}
	logger.Infof(ctx, "[KnowledgePublish] reconciled knowledge=%s reason=%s current=%s deleted_db=%d",
		knowledge.ID, reason, currentVersionID, deletedDB)
	return nil
}
