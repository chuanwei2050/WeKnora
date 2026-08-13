package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// GraphTripleReviewHandler manages staged graph triples independently from knowledge-version governance.
type GraphTripleReviewHandler struct {
	repo      interfaces.GraphTripleReviewRepository
	chunkRepo interfaces.ChunkRepository
	graph     interfaces.RetrieveGraphRepository
}

func NewGraphTripleReviewHandler(
	repo interfaces.GraphTripleReviewRepository,
	chunkRepo interfaces.ChunkRepository,
	graph interfaces.RetrieveGraphRepository,
) *GraphTripleReviewHandler {
	return &GraphTripleReviewHandler{repo: repo, chunkRepo: chunkRepo, graph: graph}
}

type graphTripleRejectRequest struct {
	Comment string `json:"comment"`
}

func (h *GraphTripleReviewHandler) tenantReviewer(c *gin.Context) (uint64, string, bool) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	userID := strings.TrimSpace(c.GetString(types.UserIDContextKey.String()))
	if tenantID == 0 || userID == "" || !canAccessGraphTripleReview(c.Request.Context()) {
		return 0, "", false
	}
	return tenantID, userID, true
}

// canAccessGraphTripleReview mirrors feedback/settings admin access:
// native WeKnora users (no BidReview role) may review; BidReview SSO needs tenant/platform admin.
func canAccessGraphTripleReview(ctx context.Context) bool {
	user, ok := types.UserFromContext(ctx)
	if !ok {
		return false
	}
	if user.BidReviewRole == "" {
		return true
	}
	return types.IsBidReviewKnowledgeAdmin(ctx)
}

func (h *GraphTripleReviewHandler) List(c *gin.Context) {
	tenantID, _, ok := h.tenantReviewer(c)
	if !ok {
		c.Error(errors.NewForbiddenError("graph triple review requires a tenant administrator"))
		return
	}
	items, err := h.repo.List(
		c.Request.Context(),
		tenantID,
		strings.TrimSpace(c.Query("knowledge_base_id")),
		types.GraphTripleReviewStatus(strings.TrimSpace(c.Query("status"))),
	)
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}

func (h *GraphTripleReviewHandler) Get(c *gin.Context) {
	tenantID, _, ok := h.tenantReviewer(c)
	if !ok {
		c.Error(errors.NewForbiddenError("graph triple review requires a tenant administrator"))
		return
	}
	item, err := h.repo.GetByID(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if item == nil {
		c.Error(errors.NewNotFoundError("graph triple candidate not found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

func (h *GraphTripleReviewHandler) Approve(c *gin.Context) {
	tenantID, userID, ok := h.tenantReviewer(c)
	if !ok {
		c.Error(errors.NewForbiddenError("graph triple review requires a tenant administrator"))
		return
	}
	ctx := c.Request.Context()
	item, err := h.repo.GetByID(ctx, tenantID, c.Param("id"))
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if item == nil {
		c.Error(errors.NewNotFoundError("graph triple candidate not found"))
		return
	}
	if !types.CanApproveGraphTriple(item.Status) {
		c.Error(errors.NewBadRequestError("only pending graph triples can be approved"))
		return
	}
	chunk, err := h.chunkRepo.GetChunkByID(ctx, tenantID, item.ChunkID)
	if err != nil || chunk == nil {
		c.Error(errors.NewNotFoundError("chunk for graph triple candidate not found"))
		return
	}
	graph := item.GraphData.AsGraphData()
	if graph == nil || len(graph.Relation) == 0 {
		c.Error(errors.NewBadRequestError("graph triple candidate has no relations"))
		return
	}
	for _, node := range graph.Node {
		if node != nil && len(node.Chunks) == 0 {
			node.Chunks = []string{chunk.ID}
		}
	}
	if err := service.WriteExtractedGraph(ctx, h.graph, chunk, graph, item.ModelID); err != nil {
		// Keep pending so approve can be retried.
		c.Error(errors.NewInternalServerError("graph write failed; candidate remains pending: " + err.Error()))
		return
	}
	if err := h.repo.MarkWritten(ctx, tenantID, item.ID, userID); err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	updated, _ := h.repo.GetByID(ctx, tenantID, item.ID)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": updated})
}

func (h *GraphTripleReviewHandler) Reject(c *gin.Context) {
	tenantID, userID, ok := h.tenantReviewer(c)
	if !ok {
		c.Error(errors.NewForbiddenError("graph triple review requires a tenant administrator"))
		return
	}
	var req graphTripleRejectRequest
	_ = c.ShouldBindJSON(&req)
	ctx := c.Request.Context()
	item, err := h.repo.GetByID(ctx, tenantID, c.Param("id"))
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if item == nil {
		c.Error(errors.NewNotFoundError("graph triple candidate not found"))
		return
	}
	if !types.CanRejectGraphTriple(item.Status) {
		c.Error(errors.NewBadRequestError("only pending graph triples can be rejected"))
		return
	}
	if err := h.repo.MarkRejected(ctx, tenantID, item.ID, userID, strings.TrimSpace(req.Comment)); err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	updated, _ := h.repo.GetByID(ctx, tenantID, item.ID)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": updated})
}
