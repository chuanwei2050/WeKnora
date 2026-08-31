package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// GraphTripleReviewRepository stores staging triples pending human review.
type GraphTripleReviewRepository interface {
	Enqueue(ctx context.Context, candidate *types.GraphTripleCandidate) error
	GetByID(ctx context.Context, tenantID uint64, id string) (*types.GraphTripleCandidate, error)
	List(ctx context.Context, tenantID uint64, knowledgeBaseID string, status types.GraphTripleReviewStatus) ([]*types.GraphTripleCandidate, error)
	MarkWritten(ctx context.Context, tenantID uint64, id, reviewerID string) error
	MarkRejected(ctx context.Context, tenantID uint64, id, reviewerID, comment string) error
	SupersedePendingByKnowledgeBase(ctx context.Context, tenantID uint64, knowledgeBaseID string) error
	SupersedePendingByKnowledgeIDs(ctx context.Context, tenantID uint64, knowledgeIDs []string) error
	MarkSuperseded(ctx context.Context, tenantID uint64, id string) error
}
