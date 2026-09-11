package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/errors"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
)

// GraphExploreHandler serves read-only knowledge-graph overview APIs and rebuild.
type GraphExploreHandler struct {
	kbService        interfaces.KnowledgeBaseService
	knowledgeService interfaces.KnowledgeService
	graphRepo        interfaces.RetrieveGraphRepository
	asynqClient      interfaces.TaskEnqueuer
}

func NewGraphExploreHandler(
	kbService interfaces.KnowledgeBaseService,
	knowledgeService interfaces.KnowledgeService,
	graphRepo interfaces.RetrieveGraphRepository,
	asynqClient interfaces.TaskEnqueuer,
) *GraphExploreHandler {
	return &GraphExploreHandler{
		kbService:        kbService,
		knowledgeService: knowledgeService,
		graphRepo:        graphRepo,
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
	if kb.TenantID != tenantID {
		c.Error(errors.NewForbiddenError("knowledge base access denied"))
		return
	}
	limit, _ := strconv.Atoi(strings.TrimSpace(c.Query("limit")))
	overview, err := h.graphRepo.GetGraphOverview(c.Request.Context(), types.GraphScope{
		TenantID:        tenantID,
		KnowledgeBaseID: kbID,
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

// Rebuild clears the KB canonical graph then re-enqueues post-process extraction.
func (h *GraphExploreHandler) Rebuild(c *gin.Context) {
	if h == nil || h.graphRepo == nil {
		c.Error(apperrors.NewInternalServerError("graph repository unavailable"))
		return
	}
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	kbID := strings.TrimSpace(c.Param("id"))
	kb, err := h.kbService.GetKnowledgeBaseByID(c.Request.Context(), kbID)
	if err != nil || kb == nil {
		c.Error(errors.NewNotFoundError("knowledge base not found"))
		return
	}
	if kb.TenantID != tenantID {
		c.Error(errors.NewForbiddenError("knowledge base access denied"))
		return
	}
	if !kb.IsGraphEnabled() {
		c.Error(errors.NewBadRequestError("knowledge graph is not enabled"))
		return
	}
	if err := h.graphRepo.DeleteCanonicalKnowledgeBase(c.Request.Context(), tenantID, kbID); err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	items, err := h.knowledgeService.ListKnowledgeByKnowledgeBaseID(c.Request.Context(), kbID)
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	enqueued := 0
	for _, item := range items {
		if item == nil || item.ParseStatus != types.ParseStatusCompleted {
			continue
		}
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
	c.JSON(http.StatusAccepted, gin.H{
		"success": true,
		"data": gin.H{
			"document_count": enqueued,
			"message":        "graph cleared; extraction tasks enqueued",
		},
	})
}
