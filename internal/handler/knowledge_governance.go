package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// KnowledgeGovernanceHandler exposes the immutable version and review
// lifecycle. Indexing itself remains owned by the existing ingestion worker;
// publish only switches visibility after the caller confirms index readiness.
type KnowledgeGovernanceHandler struct {
	repo           interfaces.KnowledgeGovernanceRepository
	knowledge      interfaces.KnowledgeService
	knowledgeBases interfaces.KnowledgeBaseService
}

func NewKnowledgeGovernanceHandler(
	repo interfaces.KnowledgeGovernanceRepository,
	knowledge interfaces.KnowledgeService,
	knowledgeBases interfaces.KnowledgeBaseService,
) *KnowledgeGovernanceHandler {
	return &KnowledgeGovernanceHandler{repo: repo, knowledge: knowledge, knowledgeBases: knowledgeBases}
}

// activateVersion coordinates the governed visibility switch. Vector and
// keyword retrieval use the database current_version_id as their shared
// visibility pointer; the graph namespace is switched first and rolled back
// if the database transaction cannot complete.
func (h *KnowledgeGovernanceHandler) activateVersion(c *gin.Context, tenantID uint64, version *types.KnowledgeVersion, now time.Time) error {
	if version == nil {
		return errors.NewNotFoundError("knowledge version not found")
	}
	if version.EffectiveAt != nil && now.Before(*version.EffectiveAt) {
		return h.repo.ActivateVersion(c.Request.Context(), tenantID, version.ID, now)
	}
	return h.repo.ActivateVersion(c.Request.Context(), tenantID, version.ID, now)
}

type createKnowledgeVersionRequest struct {
	VersionLabel   string                        `json:"version_label" binding:"required"`
	Content        string                        `json:"content,omitempty"`
	ContentHash    string                        `json:"content_hash,omitempty"`
	SnapshotRef    string                        `json:"snapshot_ref,omitempty"`
	SourceMetadata types.KnowledgeSourceMetadata `json:"source_metadata" binding:"required"`
	EffectiveAt    *time.Time                    `json:"effective_at,omitempty"`
	ExpiresAt      *time.Time                    `json:"expires_at,omitempty"`
}

type reviewKnowledgeVersionRequest struct {
	Comment string `json:"comment,omitempty"`
}

func (h *KnowledgeGovernanceHandler) tenantID(c *gin.Context) (uint64, string, bool) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	userID := strings.TrimSpace(c.GetString(types.UserIDContextKey.String()))
	return tenantID, userID, tenantID != 0 && userID != ""
}

func (h *KnowledgeGovernanceHandler) canManageKnowledge(c *gin.Context, knowledgeID string) bool {
	if h.knowledge == nil || h.knowledgeBases == nil {
		return false
	}
	knowledge, err := h.knowledge.GetKnowledgeByID(c.Request.Context(), knowledgeID)
	if err != nil || knowledge == nil {
		return false
	}
	kb, err := h.knowledgeBases.GetKnowledgeBaseByID(c.Request.Context(), knowledge.KnowledgeBaseID)
	return err == nil && kb != nil && types.CanManageKnowledgeBase(c.Request.Context(), kb)
}

func knowledgeIDFromPath(c *gin.Context) string {
	if id := c.Param("knowledge_id"); id != "" {
		return id
	}
	return c.Param("id")
}

func (h *KnowledgeGovernanceHandler) CreateVersion(c *gin.Context) {
	tenantID, userID, ok := h.tenantID(c)
	knowledgeID := knowledgeIDFromPath(c)
	if !ok || !h.canManageKnowledge(c, knowledgeID) {
		c.Error(errors.NewForbiddenError("knowledge governance permission denied"))
		return
	}
	var request createKnowledgeVersionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	metadata := request.SourceMetadata
	if err := metadata.Validate(); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	contentHash := strings.TrimSpace(request.ContentHash)
	if contentHash == "" {
		contentHash = types.HashKnowledgeContent([]byte(request.Content))
	} else if request.Content != "" && !strings.EqualFold(contentHash, types.HashKnowledgeContent([]byte(request.Content))) {
		c.Error(errors.NewBadRequestError("content_hash does not match content"))
		return
	}
	version := &types.KnowledgeVersion{
		ID: uuid.NewString(), TenantID: tenantID, KnowledgeID: knowledgeID,
		VersionLabel: request.VersionLabel, ContentHash: contentHash, SnapshotRef: request.SnapshotRef,
		SourceMetadata: metadata, Status: types.KnowledgeVersionDraft, CreatedBy: userID,
		CreatedAt: time.Now().UTC(), EffectiveAt: request.EffectiveAt, ExpiresAt: request.ExpiresAt,
	}
	existing, err := h.repo.ListVersions(c.Request.Context(), tenantID, version.KnowledgeID)
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if err := types.ValidateVersionValidityWindow(*version, existing); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	for _, prior := range existing {
		duplicate, checkErr := types.ValidateVersionUniqueness(prior, contentHash, metadata)
		if checkErr != nil {
			c.Error(errors.NewBadRequestError(checkErr.Error()))
			return
		}
		if duplicate {
			c.Error(errors.NewConflictError("an identical knowledge version already exists"))
			return
		}
	}
	knowledgeForVersion, err := h.knowledge.GetKnowledgeByID(c.Request.Context(), version.KnowledgeID)
	if err != nil || knowledgeForVersion == nil {
		c.Error(errors.NewNotFoundError("knowledge not found"))
		return
	}
	kb, err := h.knowledgeBases.GetKnowledgeBaseByID(c.Request.Context(), knowledgeForVersion.KnowledgeBaseID)
	if err != nil || kb == nil || !kb.Governance.Enabled {
		c.Error(errors.NewBadRequestError("knowledge governance is not enabled for this knowledge base"))
		return
	}
	if err := h.repo.CreateVersion(c.Request.Context(), version); err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	knowledge := knowledgeForVersion
	knowledge.PendingVersionID = version.ID
	if err := h.knowledge.UpdateKnowledge(c.Request.Context(), knowledge); err != nil {
		c.Error(errors.NewInternalServerError("failed to stage governed version"))
		return
	}
	if _, err := h.knowledge.ReparseKnowledge(c.Request.Context(), knowledge.ID); err != nil {
		c.Error(errors.NewInternalServerError("failed to enqueue governed parsing"))
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": version})
}

func (h *KnowledgeGovernanceHandler) ListVersions(c *gin.Context) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	versions, err := h.repo.ListVersions(c.Request.Context(), tenantID, knowledgeIDFromPath(c))
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": versions})
}

func (h *KnowledgeGovernanceHandler) GetVersion(c *gin.Context) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	version, err := h.repo.GetVersion(c.Request.Context(), tenantID, c.Param("version_id"))
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if version == nil || version.KnowledgeID != knowledgeIDFromPath(c) {
		c.Error(errors.NewNotFoundError("knowledge version not found"))
		return
	}
	reviews, err := h.repo.ListReviews(c.Request.Context(), version.ID)
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"version": version, "reviews": reviews}})
}

func (h *KnowledgeGovernanceHandler) transition(c *gin.Context, next types.KnowledgeVersionStatus, action string) {
	tenantID, userID, ok := h.tenantID(c)
	if !ok {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	version, err := h.repo.GetVersion(c.Request.Context(), tenantID, c.Param("version_id"))
	if err != nil || version == nil {
		c.Error(errors.NewNotFoundError("knowledge version not found"))
		return
	}
	if !h.canManageKnowledge(c, version.KnowledgeID) {
		c.Error(errors.NewForbiddenError("knowledge governance permission denied"))
		return
	}
	if err := types.ValidateKnowledgeVersionReview(version, userID, next); err != nil {
		c.Error(errors.NewForbiddenError(err.Error()))
		return
	}
	if err := types.TransitionKnowledgeVersion(version, next); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	if err := h.repo.UpdateVersionStatus(c.Request.Context(), tenantID, version.ID, next); err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	comment := ""
	var request reviewKnowledgeVersionRequest
	if c.Request.ContentLength > 0 && c.ShouldBindJSON(&request) == nil {
		comment = strings.TrimSpace(request.Comment)
	}
	if action != "" {
		if err := h.repo.CreateReview(c.Request.Context(), &types.KnowledgeVersionReview{ID: uuid.NewString(), VersionID: version.ID, ReviewerID: userID, Action: action, Comment: comment, CreatedAt: time.Now().UTC()}); err != nil {
			c.Error(errors.NewInternalServerError(err.Error()))
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": version})
}

func (h *KnowledgeGovernanceHandler) SubmitReview(c *gin.Context) {
	h.transition(c, types.KnowledgeVersionPendingReview, "submit")
}

func (h *KnowledgeGovernanceHandler) Approve(c *gin.Context) {
	h.transition(c, types.KnowledgeVersionApproved, "approve")
}

func (h *KnowledgeGovernanceHandler) Reject(c *gin.Context) {
	h.transition(c, types.KnowledgeVersionRejected, "reject")
}

func (h *KnowledgeGovernanceHandler) Publish(c *gin.Context) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	version, err := h.repo.GetVersion(c.Request.Context(), tenantID, c.Param("version_id"))
	if err != nil || version == nil {
		c.Error(errors.NewNotFoundError("knowledge version not found"))
		return
	}
	if !h.canManageKnowledge(c, version.KnowledgeID) {
		c.Error(errors.NewForbiddenError("knowledge governance permission denied"))
		return
	}
	var request struct {
		IndexReady bool `json:"index_ready"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || !request.IndexReady {
		c.Error(errors.NewBadRequestError("index_ready must be true after all indexes are ready"))
		return
	}
	if err := h.activateVersion(c, tenantID, version, time.Now().UTC()); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	version, err = h.repo.GetVersion(c.Request.Context(), tenantID, version.ID)
	if err != nil || version == nil {
		c.Error(errors.NewInternalServerError("failed to reload activated knowledge version"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": version})
}

func (h *KnowledgeGovernanceHandler) Rollback(c *gin.Context) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	version, err := h.repo.GetVersion(c.Request.Context(), tenantID, c.Param("version_id"))
	if err != nil || version == nil {
		c.Error(errors.NewNotFoundError("knowledge version not found"))
		return
	}
	if !h.canManageKnowledge(c, version.KnowledgeID) {
		c.Error(errors.NewForbiddenError("knowledge governance permission denied"))
		return
	}
	if err := h.activateVersion(c, tenantID, version, time.Now().UTC()); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	version, err = h.repo.GetVersion(c.Request.Context(), tenantID, version.ID)
	if err != nil || version == nil {
		c.Error(errors.NewInternalServerError("failed to reload rolled back knowledge version"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": version})
}
