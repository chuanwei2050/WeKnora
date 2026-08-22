package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type approvedEndpointRepository struct{ db *gorm.DB }

func NewApprovedEndpointRepository(db *gorm.DB) interfaces.ApprovedEndpointRepository {
	return &approvedEndpointRepository{db: db}
}

func (r *approvedEndpointRepository) Create(ctx context.Context, endpoint *types.ApprovedEndpoint) error {
	if endpoint.ID == "" {
		endpoint.ID = uuid.NewString()
	}
	return r.db.WithContext(ctx).Create(endpoint).Error
}

func (r *approvedEndpointRepository) GetByID(ctx context.Context, tenantID uint64, id string) (*types.ApprovedEndpoint, error) {
	var endpoint types.ApprovedEndpoint
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", types.PlatformScopeTenantID, id).First(&endpoint).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &endpoint, err
}

func (r *approvedEndpointRepository) List(ctx context.Context, tenantID uint64, category types.ApprovedEndpointCategory) ([]*types.ApprovedEndpoint, error) {
	var endpoints []*types.ApprovedEndpoint
	query := r.db.WithContext(ctx).Where("tenant_id = ?", types.PlatformScopeTenantID)
	if category != "" {
		query = query.Where("category = ?", category)
	}
	return endpoints, query.Order("created_at DESC").Find(&endpoints).Error
}

func (r *approvedEndpointRepository) Update(ctx context.Context, endpoint *types.ApprovedEndpoint) error {
	return r.db.WithContext(ctx).Model(&types.ApprovedEndpoint{}).
		Where("tenant_id = ? AND id = ?", types.PlatformScopeTenantID, endpoint.ID).
		Select("*").Updates(endpoint).Error
}

func (r *approvedEndpointRepository) Delete(ctx context.Context, tenantID uint64, id string) error {
	return r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", types.PlatformScopeTenantID, id).Delete(&types.ApprovedEndpoint{}).Error
}

func (r *approvedEndpointRepository) CreateAudit(ctx context.Context, audit *types.ApprovedEndpointAudit) error {
	if audit.ID == "" {
		audit.ID = uuid.NewString()
	}
	return r.db.WithContext(ctx).Create(audit).Error
}

func (r *approvedEndpointRepository) ListAudits(ctx context.Context, tenantID uint64, endpointID string) ([]*types.ApprovedEndpointAudit, error) {
	var audits []*types.ApprovedEndpointAudit
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND endpoint_id = ?", types.PlatformScopeTenantID, endpointID).Order("created_at DESC").Find(&audits).Error
	return audits, err
}

type answerFeedbackRepository struct{ db *gorm.DB }

func NewAnswerFeedbackRepository(db *gorm.DB) interfaces.AnswerFeedbackRepository {
	return &answerFeedbackRepository{db: db}
}

func (r *answerFeedbackRepository) Create(ctx context.Context, feedback *types.AnswerFeedback) error {
	if feedback.ID == "" {
		feedback.ID = uuid.NewString()
	}
	return r.db.WithContext(ctx).Create(feedback).Error
}

func (r *answerFeedbackRepository) CreateCandidate(ctx context.Context, candidate *types.FeedbackCandidate) error {
	if candidate.ID == "" {
		candidate.ID = uuid.NewString()
	}
	if candidate.CreatedAt.IsZero() {
		candidate.CreatedAt = time.Now().UTC()
	}
	payload, err := json.Marshal(candidate.Payload)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Table("feedback_candidates").Create(map[string]interface{}{
		"id": candidate.ID, "tenant_id": candidate.TenantID, "feedback_id": candidate.FeedbackID,
		"target": candidate.Target, "status": candidate.Status, "payload": payload, "created_at": candidate.CreatedAt,
	}).Error
}

func (r *answerFeedbackRepository) GetCandidateByFeedback(ctx context.Context, tenantID uint64, feedbackID string) (*types.FeedbackCandidate, error) {
	var row struct {
		ID         string
		TenantID   uint64
		FeedbackID string
		Target     types.FeedbackTarget
		Status     types.FeedbackCandidateStatus
		Payload    types.JSON
		CreatedAt  time.Time
	}
	err := r.db.WithContext(ctx).Table("feedback_candidates").Where("tenant_id = ? AND feedback_id = ?", tenantID, feedbackID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	payload, payloadErr := row.Payload.Map()
	if payloadErr != nil {
		return nil, payloadErr
	}
	return &types.FeedbackCandidate{ID: row.ID, TenantID: row.TenantID, FeedbackID: row.FeedbackID, Target: row.Target, Status: row.Status, Payload: payload, CreatedAt: row.CreatedAt}, nil
}

func (r *answerFeedbackRepository) GetByID(ctx context.Context, tenantID uint64, id string) (*types.AnswerFeedback, error) {
	var feedback types.AnswerFeedback
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).First(&feedback).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &feedback, err
}

func (r *answerFeedbackRepository) ListByTenant(ctx context.Context, tenantID uint64, status types.FeedbackStatus) ([]*types.AnswerFeedback, error) {
	var feedback []*types.AnswerFeedback
	query := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	return feedback, query.Order("created_at DESC").Find(&feedback).Error
}

func (r *answerFeedbackRepository) Update(ctx context.Context, feedback *types.AnswerFeedback) error {
	return r.db.WithContext(ctx).Model(&types.AnswerFeedback{}).
		Where("tenant_id = ? AND id = ?", feedback.TenantID, feedback.ID).
		Select("*").Updates(feedback).Error
}
