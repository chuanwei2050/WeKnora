package handler

import (
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type AnswerFeedbackHandler struct {
	repo           interfaces.AnswerFeedbackRepository
	messageService interfaces.MessageService
	governanceRepo interfaces.KnowledgeGovernanceRepository
	benchmarkRepo  interfaces.AcceptanceBenchmarkRepository
}

func NewAnswerFeedbackHandler(repo interfaces.AnswerFeedbackRepository, messageService interfaces.MessageService, governanceRepo interfaces.KnowledgeGovernanceRepository, benchmarkRepo interfaces.AcceptanceBenchmarkRepository) *AnswerFeedbackHandler {
	return &AnswerFeedbackHandler{repo: repo, messageService: messageService, governanceRepo: governanceRepo, benchmarkRepo: benchmarkRepo}
}

type answerFeedbackRequest struct {
	SessionID     string `json:"session_id" binding:"required"`
	MessageID     string `json:"message_id" binding:"required"`
	AnswerVersion string `json:"answer_version" binding:"required"`
	Rating        int    `json:"rating" binding:"required"`
	Correction    string `json:"correction,omitempty"`
}

type answerFeedbackReviewRequest struct {
	Status              types.FeedbackStatus `json:"status" binding:"required"`
	Target              types.FeedbackTarget `json:"target" binding:"required"`
	KnowledgeGovernance bool                 `json:"knowledge_governance"`
	AcceptanceBenchmark bool                 `json:"acceptance_benchmark"`
	ImprovementTicket   bool                 `json:"improvement_ticket"`
}

func (h *AnswerFeedbackHandler) Submit(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	var request answerFeedbackRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	message, err := h.messageService.GetMessage(ctx, strings.TrimSpace(request.SessionID), strings.TrimSpace(request.MessageID))
	if err != nil || message == nil || message.Role != "assistant" || !message.IsCompleted {
		c.Error(errors.NewNotFoundError("completed assistant message not found"))
		return
	}
	feedback := &types.AnswerFeedback{TenantID: tenantID, SessionID: request.SessionID, MessageID: request.MessageID, AnswerVersion: request.AnswerVersion, Rating: request.Rating, Correction: request.Correction, Status: types.FeedbackPending}
	if err := types.ValidateFeedback(*feedback); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	if err := h.repo.Create(ctx, feedback); err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": feedback})
}

func (h *AnswerFeedbackHandler) List(c *gin.Context) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 || !types.IsBidReviewKnowledgeAdmin(c.Request.Context()) {
		c.Error(errors.NewForbiddenError("feedback review requires a tenant administrator"))
		return
	}
	feedback, err := h.repo.ListByTenant(c.Request.Context(), tenantID, types.FeedbackStatus(strings.TrimSpace(c.Query("status"))))
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": feedback})
}

func (h *AnswerFeedbackHandler) Review(c *gin.Context) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	reviewerID := strings.TrimSpace(c.GetString(types.UserIDContextKey.String()))
	if tenantID == 0 || reviewerID == "" {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	if !types.IsBidReviewKnowledgeAdmin(c.Request.Context()) {
		c.Error(errors.NewForbiddenError("feedback review requires a tenant administrator"))
		return
	}
	feedback, err := h.repo.GetByID(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil || feedback == nil {
		c.Error(errors.NewNotFoundError("feedback not found"))
		return
	}
	var request answerFeedbackReviewRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	if err := types.ValidateFeedbackTarget(request.Target); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	if request.Status != types.FeedbackAccepted && request.Status != types.FeedbackRejected {
		c.Error(errors.NewBadRequestError("review status must be accepted or rejected"))
		return
	}
	capabilities := types.FeedbackCapabilities{KnowledgeGovernance: h.governanceRepo != nil, AcceptanceBenchmark: h.benchmarkRepo != nil}
	if request.Status == types.FeedbackAccepted {
		if err := types.ValidateFeedbackAdoption(feedback.Status, request.Target, capabilities); err != nil {
			c.Error(errors.NewBadRequestError(err.Error()))
			return
		}
	}
	feedback.Target, feedback.Status, feedback.ReviewerID = request.Target, request.Status, reviewerID
	if request.Status == types.FeedbackAccepted {
		candidate, candidateErr := h.repo.GetCandidateByFeedback(c.Request.Context(), tenantID, feedback.ID)
		if candidateErr != nil {
			c.Error(errors.NewInternalServerError(candidateErr.Error()))
			return
		}
		if candidate == nil {
			candidate = &types.FeedbackCandidate{ID: uuid.NewString(), TenantID: tenantID, FeedbackID: feedback.ID, Target: request.Target, Status: types.FeedbackCandidatePendingReview, Payload: map[string]interface{}{
				"session_id": feedback.SessionID, "message_id": feedback.MessageID, "answer_version": feedback.AnswerVersion, "correction": feedback.Correction,
			}}
			if err := h.repo.CreateCandidate(c.Request.Context(), candidate); err != nil {
				candidate, candidateErr = h.repo.GetCandidateByFeedback(c.Request.Context(), tenantID, feedback.ID)
				if candidateErr != nil || candidate == nil {
					c.Error(errors.NewInternalServerError(err.Error()))
					return
				}
			}
		}
		if candidate.Target != request.Target {
			c.Error(errors.NewBadRequestError("feedback has already been adopted to another target"))
			return
		}
		feedback.CandidateID = candidate.ID
	}
	if err := h.repo.Update(c.Request.Context(), feedback); err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": feedback})
}
