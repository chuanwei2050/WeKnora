package handler

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ApprovedEndpointHandler struct {
	repo interfaces.ApprovedEndpointRepository
}

func NewApprovedEndpointHandler(repo interfaces.ApprovedEndpointRepository) *ApprovedEndpointHandler {
	return &ApprovedEndpointHandler{repo: repo}
}

func (h *ApprovedEndpointHandler) audit(c *gin.Context, endpoint *types.ApprovedEndpoint, actorID, action string, before, after interface{}) error {
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		return err
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		return err
	}
	return h.repo.CreateAudit(c.Request.Context(), &types.ApprovedEndpointAudit{TenantID: endpoint.TenantID, EndpointID: endpoint.ID, ActorID: actorID, Action: action, BeforeJSON: string(beforeJSON), AfterJSON: string(afterJSON), CreatedAt: time.Now().UTC()})
}

func (h *ApprovedEndpointHandler) Create(c *gin.Context) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	userID := strings.TrimSpace(c.GetString(types.UserIDContextKey.String()))
	if tenantID == 0 || userID == "" {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	if !types.IsBidReviewKnowledgeAdmin(c.Request.Context()) {
		c.Error(errors.NewForbiddenError("approved endpoint management requires a tenant administrator"))
		return
	}
	var endpoint types.ApprovedEndpoint
	if err := c.ShouldBindJSON(&endpoint); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	endpoint.ID, endpoint.TenantID, endpoint.CreatedBy = uuid.NewString(), tenantID, userID
	if err := endpoint.Validate(); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	if err := validateApprovedEndpointDeployment(&endpoint); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	if err := h.repo.Create(c.Request.Context(), &endpoint); err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if err := h.audit(c, &endpoint, userID, "create", nil, endpoint); err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": endpoint})
}

func (h *ApprovedEndpointHandler) List(c *gin.Context) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	items, err := h.repo.List(c.Request.Context(), tenantID, types.ApprovedEndpointCategory(strings.TrimSpace(c.Query("category"))))
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}

func (h *ApprovedEndpointHandler) Get(c *gin.Context) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	item, err := h.repo.GetByID(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if item == nil {
		c.Error(errors.NewNotFoundError("approved endpoint not found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

func (h *ApprovedEndpointHandler) Audits(c *gin.Context) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	if !types.IsBidReviewKnowledgeAdmin(c.Request.Context()) {
		c.Error(errors.NewForbiddenError("approved endpoint audits require a tenant administrator"))
		return
	}
	audits, err := h.repo.ListAudits(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": audits})
}

func (h *ApprovedEndpointHandler) Update(c *gin.Context) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 || !types.IsBidReviewKnowledgeAdmin(c.Request.Context()) {
		c.Error(errors.NewForbiddenError("approved endpoint management requires a tenant administrator"))
		return
	}
	item, err := h.repo.GetByID(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil || item == nil {
		c.Error(errors.NewNotFoundError("approved endpoint not found"))
		return
	}
	var update types.ApprovedEndpoint
	if err := c.ShouldBindJSON(&update); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	update.ID, update.TenantID, update.CreatedBy = item.ID, item.TenantID, item.CreatedBy
	if err := update.Validate(); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	if err := validateApprovedEndpointDeployment(&update); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	if err := h.repo.Update(c.Request.Context(), &update); err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	actorID := strings.TrimSpace(c.GetString(types.UserIDContextKey.String()))
	if err := h.audit(c, &update, actorID, "update", item, update); err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": update})
}

func validateApprovedEndpointDeployment(endpoint *types.ApprovedEndpoint) error {
	if endpoint == nil || !strings.EqualFold(strings.TrimSpace(os.Getenv("AIR_GAPPED_MODE")), "true") {
		return nil
	}
	ips, err := net.LookupIP(endpoint.Host)
	if err != nil {
		return fmt.Errorf("resolve approved endpoint host: %w", err)
	}
	if err := endpoint.ValidateDeploymentAllowlist(secutils.IsSSRFWhitelisted, ips, true); err != nil {
		return fmt.Errorf("approved endpoint deployment allowlist: %w", err)
	}
	return nil
}

func (h *ApprovedEndpointHandler) Delete(c *gin.Context) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	actorID := strings.TrimSpace(c.GetString(types.UserIDContextKey.String()))
	if tenantID == 0 {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	if !types.IsBidReviewKnowledgeAdmin(c.Request.Context()) {
		c.Error(errors.NewForbiddenError("approved endpoint management requires a tenant administrator"))
		return
	}
	item, err := h.repo.GetByID(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil || item == nil {
		c.Error(errors.NewNotFoundError("approved endpoint not found"))
		return
	}
	if err := h.repo.Delete(c.Request.Context(), tenantID, c.Param("id")); err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if err := h.audit(c, item, actorID, "delete", item, nil); err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.Status(http.StatusNoContent)
}
