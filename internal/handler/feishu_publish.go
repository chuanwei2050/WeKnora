package handler

import (
	"errors"
	"net/http"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// FeishuPublishHandler exposes Feishu knowledge publish HTTP APIs.
type FeishuPublishHandler struct {
	service   interfaces.FeishuPublishService
	kbService interfaces.KnowledgeBaseService
}

// NewFeishuPublishHandler constructs the handler.
func NewFeishuPublishHandler(
	svc interfaces.FeishuPublishService,
	kbService interfaces.KnowledgeBaseService,
) *FeishuPublishHandler {
	return &FeishuPublishHandler{service: svc, kbService: kbService}
}

func (h *FeishuPublishHandler) tenantID(c *gin.Context) uint64 {
	return c.GetUint64(types.TenantIDContextKey.String())
}

func (h *FeishuPublishHandler) writeErr(c *gin.Context, err error) {
	if err == nil {
		return
	}
	status := http.StatusBadRequest
	code := "bad_request"
	msg := err.Error()
	hint := ""
	switch {
	case errors.Is(err, service.ErrFeishuPublishDisabled):
		status = http.StatusNotFound
		code = "feature_disabled"
		msg = "feishu knowledge sync is not enabled"
	case errors.Is(err, service.ErrFeishuPublishForbidden):
		status = http.StatusForbidden
		code = "forbidden"
		msg = "access denied"
	case errors.Is(err, service.ErrFeishuPublishKBType):
		status = http.StatusBadRequest
		code = "unsupported_kb_type"
	case errors.Is(err, service.ErrFeishuPublishSecretReq):
		code = "secret_required"
		hint = "provide app_secret when app_id changes or no secret is stored"
	case errors.Is(err, service.ErrFeishuPublishLoopOverlap):
		status = http.StatusConflict
		code = "loop_overlap"
		hint = "publish target overlaps an existing Feishu data source for this knowledge base"
	case errors.Is(err, service.ErrFeishuPublishSpaceLocked):
		status = http.StatusConflict
		code = "space_locked"
		hint = "unbind before switching to another wiki space"
	case errors.Is(err, service.ErrFeishuPublishActiveRun):
		status = http.StatusConflict
		code = "active_run"
	case errors.Is(err, service.ErrFeishuPublishParentMiss):
		code = "parent_mismatch"
	case errors.Is(err, service.ErrFeishuPublishEndpoint):
		status = http.StatusBadRequest
		code = "endpoint_forbidden"
	}
	c.JSON(status, types.FeishuPublishError{Code: code, Message: msg, Hint: hint})
}

// GetConfig godoc
// @Summary Get Feishu publish config (secrets redacted)
// @Router /api/v1/knowledge-bases/{id}/feishu-publish/config [get]
func (h *FeishuPublishHandler) GetConfig(c *gin.Context) {
	view, err := h.service.GetConfigView(c.Request.Context(), h.tenantID(c), c.Param("id"))
	if err != nil {
		h.writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}

// DiscoverSpaces godoc
// @Summary Discover accessible wiki spaces without persisting credentials
// @Router /api/v1/knowledge-bases/{id}/feishu-publish/spaces [post]
func (h *FeishuPublishHandler) DiscoverSpaces(c *gin.Context) {
	var req types.FeishuPublishDiscoverSpacesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, types.FeishuPublishError{Code: "bad_request", Message: "invalid request body"})
		return
	}
	resp, err := h.service.DiscoverSpaces(c.Request.Context(), h.tenantID(c), c.Param("id"), &req)
	if err != nil {
		h.writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// ListNodes godoc
// @Summary Lazily list wiki nodes for sync location picker
// @Router /api/v1/knowledge-bases/{id}/feishu-publish/nodes [post]
func (h *FeishuPublishHandler) ListNodes(c *gin.Context) {
	var req types.FeishuPublishListNodesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, types.FeishuPublishError{Code: "bad_request", Message: "invalid request body"})
		return
	}
	resp, err := h.service.ListNodes(c.Request.Context(), h.tenantID(c), c.Param("id"), &req)
	if err != nil {
		h.writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// PreviewSync godoc
// @Summary Preview Feishu publish impact without persisting config
// @Router /api/v1/knowledge-bases/{id}/feishu-publish/preview [post]
func (h *FeishuPublishHandler) PreviewSync(c *gin.Context) {
	var req types.FeishuPublishSyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, types.FeishuPublishError{Code: "bad_request", Message: "invalid request body"})
		return
	}
	resp, err := h.service.PreviewSync(c.Request.Context(), h.tenantID(c), c.Param("id"), &req)
	if err != nil {
		h.writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// ConfirmSync godoc
// @Summary Confirm preview and queue an async publish run
// @Router /api/v1/knowledge-bases/{id}/feishu-publish/confirm [post]
func (h *FeishuPublishHandler) ConfirmSync(c *gin.Context) {
	var req types.FeishuPublishSyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, types.FeishuPublishError{Code: "bad_request", Message: "invalid request body"})
		return
	}
	resp, err := h.service.ConfirmSync(c.Request.Context(), h.tenantID(c), c.Param("id"), &req)
	if err != nil {
		h.writeErr(c, err)
		return
	}
	status := http.StatusOK
	if resp != nil && resp.Accepted {
		status = http.StatusAccepted
	}
	c.JSON(status, resp)
}

// Unbind godoc
// @Summary Unbind Feishu publish config without deleting remote pages
// @Router /api/v1/knowledge-bases/{id}/feishu-publish/unbind [post]
func (h *FeishuPublishHandler) Unbind(c *gin.Context) {
	if err := h.service.Unbind(c.Request.Context(), h.tenantID(c), c.Param("id")); err != nil {
		h.writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GetRun godoc
// @Summary Get a Feishu publish run by id
// @Router /api/v1/knowledge-bases/{id}/feishu-publish/runs/{run_id} [get]
func (h *FeishuPublishHandler) GetRun(c *gin.Context) {
	run, err := h.service.GetRun(c.Request.Context(), h.tenantID(c), c.Param("id"), c.Param("run_id"))
	if err != nil {
		h.writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, run)
}
