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
	_, kb, ok := h.knowledgeScope(c, knowledgeID)
	return ok && types.CanManageKnowledgeBase(c.Request.Context(), kb)
}

func (h *KnowledgeGovernanceHandler) knowledgeScope(c *gin.Context, knowledgeID string) (*types.Knowledge, *types.KnowledgeBase, bool) {
	if h.knowledge == nil || h.knowledgeBases == nil {
		return nil, nil, false
	}
	knowledge, err := h.knowledge.GetKnowledgeByID(c.Request.Context(), knowledgeID)
	if err != nil || knowledge == nil {
		return nil, nil, false
	}
	kb, err := h.knowledgeBases.GetKnowledgeBaseByID(c.Request.Context(), knowledge.KnowledgeBaseID)
	return knowledge, kb, err == nil && kb != nil
}

func (h *KnowledgeGovernanceHandler) canReadGovernance(c *gin.Context, knowledge *types.Knowledge, kb *types.KnowledgeBase) bool {
	if knowledge == nil || kb == nil {
		return false
	}
	userID, ok := types.UserIDFromContext(c.Request.Context())
	return types.CanManageKnowledgeBase(c.Request.Context(), kb) || types.CanReviewKnowledge(c.Request.Context(), kb) || (ok && knowledge.CreatedBy == userID)
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
	knowledge, kb, scopeOK := h.knowledgeScope(c, knowledgeID)
	canCreate := scopeOK && (types.CanManageKnowledgeBase(c.Request.Context(), kb) || (knowledge.CreatedBy == userID && types.CanContributeKnowledge(c.Request.Context(), kb)))
	if !ok || !canCreate {
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
	kb, err = h.knowledgeBases.GetKnowledgeBaseByID(c.Request.Context(), knowledgeForVersion.KnowledgeBaseID)
	if err != nil || kb == nil || !kb.Governance.Enabled {
		c.Error(errors.NewBadRequestError("knowledge governance is not enabled for this knowledge base"))
		return
	}
	if err := h.repo.CreateVersion(c.Request.Context(), version); err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	knowledgeForVersion.PendingVersionID = version.ID
	if err := h.knowledge.UpdateKnowledge(c.Request.Context(), knowledgeForVersion); err != nil {
		c.Error(errors.NewInternalServerError("failed to stage governed version"))
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
	knowledge, kb, ok := h.knowledgeScope(c, knowledgeIDFromPath(c))
	if !ok || !h.canReadGovernance(c, knowledge, kb) {
		c.Error(errors.NewForbiddenError("knowledge governance permission denied"))
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
	knowledge, kb, ok := h.knowledgeScope(c, knowledgeIDFromPath(c))
	if !ok || !h.canReadGovernance(c, knowledge, kb) {
		c.Error(errors.NewForbiddenError("knowledge governance permission denied"))
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
	knowledge, kb, scopeOK := h.knowledgeScope(c, version.KnowledgeID)
	if !scopeOK {
		c.Error(errors.NewNotFoundError("knowledge not found"))
		return
	}
	canAct := false
	switch action {
	case "submit", "withdraw":
		canAct = knowledge.CreatedBy == userID && version.CreatedBy == userID && types.CanContributeKnowledge(c.Request.Context(), kb)
	case "approve", "reject":
		canAct = types.CanReviewKnowledge(c.Request.Context(), kb)
	}
	if !canAct {
		c.Error(errors.NewForbiddenError("knowledge governance permission denied"))
		return
	}
	if action == "submit" && version.Status == next {
		if err := h.repo.TransitionVersionWithReview(c.Request.Context(), tenantID, version.ID, next, &types.KnowledgeVersionReview{
			ID: uuid.NewString(), VersionID: version.ID, ReviewerID: userID, Action: action, CreatedAt: time.Now().UTC(),
		}); err != nil {
			c.Error(errors.NewConflictError(err.Error()))
			return
		}
		version, _ = h.repo.GetVersion(c.Request.Context(), tenantID, version.ID)
		c.JSON(http.StatusOK, gin.H{"success": true, "data": version})
		return
	}
	if action == "submit" && types.CanManageKnowledgeBase(c.Request.Context(), kb) {
		if err := h.repo.PrepareManagedUpload(c.Request.Context(), tenantID, knowledge.ID, version.ID, userID); err != nil {
			c.Error(errors.NewConflictError(err.Error()))
			return
		}
		if _, err := h.knowledge.ReparseKnowledge(c.Request.Context(), knowledge.ID); err != nil {
			_ = h.repo.UpdateVersionStatus(c.Request.Context(), tenantID, version.ID, types.KnowledgeVersionPublishFailed)
			c.Error(errors.NewConflictError("managed upload parsing could not be started: " + err.Error()))
			return
		}
		version, _ = h.repo.GetVersion(c.Request.Context(), tenantID, version.ID)
		c.JSON(http.StatusOK, gin.H{"success": true, "data": version})
		return
	}
	if action == "submit" && version.Status == types.KnowledgeVersionRejected {
		version.Status = types.KnowledgeVersionDraft
	}
	if err := types.ValidateKnowledgeVersionReview(version, userID, next); err != nil {
		c.Error(errors.NewForbiddenError(err.Error()))
		return
	}
	if err := types.TransitionKnowledgeVersion(version, next); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	comment := ""
	var request reviewKnowledgeVersionRequest
	if c.Request.ContentLength > 0 && c.ShouldBindJSON(&request) == nil {
		comment = strings.TrimSpace(request.Comment)
	}
	if err := h.repo.TransitionVersionWithReview(c.Request.Context(), tenantID, version.ID, next, &types.KnowledgeVersionReview{ID: uuid.NewString(), VersionID: version.ID, ReviewerID: userID, Action: action, Comment: comment, CreatedAt: time.Now().UTC()}); err != nil {
		c.Error(errors.NewConflictError(err.Error()))
		return
	}
	if action == "approve" {
		if err := h.repo.UpdateVersionStatus(c.Request.Context(), tenantID, version.ID, types.KnowledgeVersionIndexing); err != nil {
			c.Error(errors.NewConflictError(err.Error()))
			return
		}
		if _, err := h.knowledge.ReparseKnowledge(c.Request.Context(), knowledge.ID); err != nil {
			_ = h.repo.UpdateVersionStatus(c.Request.Context(), tenantID, version.ID, types.KnowledgeVersionPublishFailed)
			c.Error(errors.NewConflictError("approved but parsing could not be started: " + err.Error()))
			return
		}
	}
	version, _ = h.repo.GetVersion(c.Request.Context(), tenantID, version.ID)
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

func (h *KnowledgeGovernanceHandler) Withdraw(c *gin.Context) {
	h.transition(c, types.KnowledgeVersionDraft, "withdraw")
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
