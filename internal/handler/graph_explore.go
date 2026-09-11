package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/errors"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
)

// GraphExploreHandler serves read-only knowledge-graph overview APIs and rebuild.
type GraphExploreHandler struct {
	kbService        interfaces.KnowledgeBaseService
	knowledgeService interfaces.KnowledgeService
	chunkService     interfaces.ChunkService
	graphRepo        interfaces.RetrieveGraphRepository
	tripleReviewRepo interfaces.GraphTripleReviewRepository
	rebuildProgress  *service.GraphRebuildProgressStore
	asynqClient      interfaces.TaskEnqueuer
}

func NewGraphExploreHandler(
	kbService interfaces.KnowledgeBaseService,
	knowledgeService interfaces.KnowledgeService,
	chunkService interfaces.ChunkService,
	graphRepo interfaces.RetrieveGraphRepository,
	tripleReviewRepo interfaces.GraphTripleReviewRepository,
	rebuildProgress *service.GraphRebuildProgressStore,
	asynqClient interfaces.TaskEnqueuer,
) *GraphExploreHandler {
	return &GraphExploreHandler{
		kbService:        kbService,
		knowledgeService: knowledgeService,
		chunkService:     chunkService,
		graphRepo:        graphRepo,
		tripleReviewRepo: tripleReviewRepo,
		rebuildProgress:  rebuildProgress,
		asynqClient:      asynqClient,
	}
}

// Overview returns a read-only graph canvas for a knowledge base.
func (h *GraphExploreHandler) Overview(c *gin.Context) {
	if h == nil || h.graphRepo == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": types.GraphOverview{
				Nodes: []types.GraphOverviewNode{},
				Edges: []types.GraphOverviewEdge{},
				Stats: types.GraphOverviewStats{EntityTypes: map[string]int{}},
			},
		})
		return
	}
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	kbID := strings.TrimSpace(c.Param("id"))
	if kbID == "" {
		c.Error(errors.NewBadRequestError("knowledge base id is required"))
		return
	}
	kb, err := h.kbService.GetKnowledgeBaseByID(c.Request.Context(), kbID)
	if err != nil || kb == nil {
		c.Error(errors.NewNotFoundError("knowledge base not found"))
		return
	}
	if !types.CanReadKnowledgeBase(c.Request.Context(), kb) {
		c.Error(errors.NewForbiddenError("knowledge base access denied"))
		return
	}
	limit, _ := strconv.Atoi(strings.TrimSpace(c.Query("limit")))
	versionByKnowledge := map[string]string{}
	if h.knowledgeService != nil {
		items, listErr := h.knowledgeService.ListKnowledgeByKnowledgeBaseID(c.Request.Context(), kbID)
		if listErr != nil {
			logger.Warnf(c.Request.Context(), "graph overview: list knowledge for namespaces: %v", listErr)
		} else {
			for _, item := range items {
				if item == nil {
					continue
				}
				// Prefer current published version; also keep pending so staging extracts remain visible while publishing.
				if vid := strings.TrimSpace(item.CurrentVersionID); vid != "" {
					versionByKnowledge[item.ID] = vid
				} else if vid := strings.TrimSpace(item.PendingVersionID); vid != "" {
					versionByKnowledge[item.ID] = vid
				}
			}
		}
	}
	overview, err := h.graphRepo.GetGraphOverview(c.Request.Context(), types.GraphScope{
		TenantID:                 tenantID,
		KnowledgeBaseID:          kbID,
		CurrentKnowledgeVersions: versionByKnowledge,
	}, limit)
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if overview == nil {
		overview = &types.GraphOverview{
			Nodes: []types.GraphOverviewNode{},
			Edges: []types.GraphOverviewEdge{},
			Stats: types.GraphOverviewStats{EntityTypes: map[string]int{}},
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": overview})
}

// RebuildStatus returns async rebuild progress for the knowledge base.
func (h *GraphExploreHandler) RebuildStatus(c *gin.Context) {
	tenantID, kb, ok := h.authorizeKB(c)
	if !ok {
		return
	}
	var pending int64
	if h.tripleReviewRepo != nil {
		pending, _ = h.tripleReviewRepo.CountByStatus(c.Request.Context(), tenantID, kb.ID, types.GraphTriplePending)
	}
	progress, err := h.rebuildProgress.RefreshFinalState(c.Request.Context(), tenantID, kb.ID, pending)
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if progress == nil {
		progress = &types.GraphRebuildProgress{Status: types.GraphRebuildIdle}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": progress})
}

// Rebuild clears the KB canonical graph then re-enqueues post-process extraction.
func (h *GraphExploreHandler) Rebuild(c *gin.Context) {
	if h == nil || h.graphRepo == nil {
		c.Error(apperrors.NewInternalServerError("graph repository unavailable"))
		return
	}
	if h.chunkService == nil {
		c.Error(apperrors.NewInternalServerError("chunk service unavailable"))
		return
	}
	tenantID, kb, ok := h.authorizeKB(c)
	if !ok {
		return
	}
	kbID := kb.ID
	if !kb.IsGraphEnabled() {
		c.Error(errors.NewBadRequestError("knowledge graph is not enabled"))
		return
	}
	items, err := h.knowledgeService.ListKnowledgeByKnowledgeBaseID(c.Request.Context(), kbID)
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	// Count eligibility before wiping the graph so a list failure cannot leave an empty KB.
	completed := make([]*types.Knowledge, 0)
	extractTotal := 0
	for _, item := range items {
		if item == nil || item.ParseStatus != types.ParseStatusCompleted {
			continue
		}
		chunks, chunkErr := h.chunkService.ListChunksByKnowledgeID(c.Request.Context(), item.ID)
		if chunkErr != nil {
			c.Error(errors.NewInternalServerError(fmt.Sprintf("list chunks for %s: %v", item.ID, chunkErr)))
			return
		}
		extractTotal += service.CountEligibleGraphExtractChunks(kb, chunks)
		completed = append(completed, item)
	}

	if err := h.graphRepo.DeleteCanonicalKnowledgeBase(c.Request.Context(), tenantID, kbID); err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if h.tripleReviewRepo != nil {
		_ = h.tripleReviewRepo.SupersedePendingByKnowledgeBase(c.Request.Context(), tenantID, kbID)
	}

	enqueued := 0
	for _, item := range completed {
		payload, err := json.Marshal(types.KnowledgePostProcessPayload{
			TenantID: tenantID, KnowledgeID: item.ID, KnowledgeBaseID: kbID,
		})
		if err != nil {
			c.Error(errors.NewInternalServerError(err.Error()))
			return
		}
		task := asynq.NewTask(types.TypeKnowledgePostProcess, payload, asynq.Queue("default"), asynq.MaxRetry(3))
		if _, err = h.asynqClient.Enqueue(task); err != nil {
			c.Error(errors.NewInternalServerError(err.Error()))
			return
		}
		enqueued++
	}
	requireReview := kb.ExtractConfig != nil && kb.ExtractConfig.RequireTripleReview
	if err := h.rebuildProgress.Start(c.Request.Context(), tenantID, kbID, extractTotal, enqueued, requireReview); err != nil {
		logger.Warnf(c.Request.Context(), "rebuild-graph: failed to persist progress: %v", err)
	}
	c.JSON(http.StatusAccepted, gin.H{
		"success": true,
		"data": gin.H{
			"document_count": enqueued,
			"extract_total":  extractTotal,
			"require_review": requireReview,
			"message":        "graph cleared; extraction tasks enqueued",
		},
	})
}

func (h *GraphExploreHandler) authorizeKB(c *gin.Context) (uint64, *types.KnowledgeBase, bool) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return 0, nil, false
	}
	kbID := strings.TrimSpace(c.Param("id"))
	kb, err := h.kbService.GetKnowledgeBaseByID(c.Request.Context(), kbID)
	if err != nil || kb == nil {
		c.Error(errors.NewNotFoundError("knowledge base not found"))
		return 0, nil, false
	}
	if !types.CanManageKnowledgeBase(c.Request.Context(), kb) {
		c.Error(errors.NewForbiddenError("knowledge base access denied"))
		return 0, nil, false
	}
	return tenantID, kb, true
}
