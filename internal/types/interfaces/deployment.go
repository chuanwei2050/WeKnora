package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// ApprovedEndpointRepository stores tenant-scoped endpoint approvals. Callers
// must still revalidate DNS/IP and purpose on every connection.
type ApprovedEndpointRepository interface {
	Create(ctx context.Context, endpoint *types.ApprovedEndpoint) error
	GetByID(ctx context.Context, tenantID uint64, id string) (*types.ApprovedEndpoint, error)
	List(ctx context.Context, tenantID uint64, category types.ApprovedEndpointCategory) ([]*types.ApprovedEndpoint, error)
	Update(ctx context.Context, endpoint *types.ApprovedEndpoint) error
	Delete(ctx context.Context, tenantID uint64, id string) error
	CreateAudit(ctx context.Context, audit *types.ApprovedEndpointAudit) error
	ListAudits(ctx context.Context, tenantID uint64, endpointID string) ([]*types.ApprovedEndpointAudit, error)
}

type AnswerFeedbackRepository interface {
	Create(ctx context.Context, feedback *types.AnswerFeedback) error
	GetByID(ctx context.Context, tenantID uint64, id string) (*types.AnswerFeedback, error)
	ListByTenant(ctx context.Context, tenantID uint64, status types.FeedbackStatus) ([]*types.AnswerFeedback, error)
	Update(ctx context.Context, feedback *types.AnswerFeedback) error
	CreateCandidate(ctx context.Context, candidate *types.FeedbackCandidate) error
	GetCandidateByFeedback(ctx context.Context, tenantID uint64, feedbackID string) (*types.FeedbackCandidate, error)
}
