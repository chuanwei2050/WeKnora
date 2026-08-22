package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type graphTripleReviewRepository struct {
	db *gorm.DB
}

func NewGraphTripleReviewRepository(db *gorm.DB) interfaces.GraphTripleReviewRepository {
	return &graphTripleReviewRepository{db: db}
}

func (r *graphTripleReviewRepository) Enqueue(ctx context.Context, candidate *types.GraphTripleCandidate) error {
	if candidate == nil {
		return fmt.Errorf("candidate is required")
	}
	if candidate.ID == "" {
		candidate.ID = uuid.NewString()
	}
	if candidate.Status == "" {
		candidate.Status = types.GraphTriplePending
	}
	if candidate.CreatedAt.IsZero() {
		candidate.CreatedAt = time.Now().UTC()
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		if err := tx.Model(&types.GraphTripleCandidate{}).
			Where("tenant_id = ? AND chunk_id = ? AND status = ?", candidate.TenantID, candidate.ChunkID, types.GraphTriplePending).
			Updates(map[string]interface{}{
				"status":        types.GraphTripleSuperseded,
				"superseded_by": candidate.ID,
				"reviewed_at":   now,
			}).Error; err != nil {
			return err
		}
		return tx.Create(candidate).Error
	})
}

func (r *graphTripleReviewRepository) GetByID(ctx context.Context, tenantID uint64, id string) (*types.GraphTripleCandidate, error) {
	var candidate types.GraphTripleCandidate
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).First(&candidate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &candidate, err
}

func (r *graphTripleReviewRepository) List(ctx context.Context, tenantID uint64, knowledgeBaseID string, status types.GraphTripleReviewStatus) ([]*types.GraphTripleCandidate, error) {
	q := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID)
	if knowledgeBaseID != "" {
		q = q.Where("knowledge_base_id = ?", knowledgeBaseID)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var items []*types.GraphTripleCandidate
	err := q.Order("created_at DESC").Limit(200).Find(&items).Error
	return items, err
}

func (r *graphTripleReviewRepository) MarkWritten(ctx context.Context, tenantID uint64, id, reviewerID string) error {
	return r.transitionPending(ctx, tenantID, id, types.GraphTripleWritten, reviewerID, "", true)
}

func (r *graphTripleReviewRepository) MarkRejected(ctx context.Context, tenantID uint64, id, reviewerID, comment string) error {
	return r.transitionPending(ctx, tenantID, id, types.GraphTripleRejected, reviewerID, comment, false)
}

func (r *graphTripleReviewRepository) SupersedePendingByKnowledgeBase(ctx context.Context, tenantID uint64, knowledgeBaseID string) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Model(&types.GraphTripleCandidate{}).
		Where("tenant_id = ? AND knowledge_base_id = ? AND status = ?", tenantID, knowledgeBaseID, types.GraphTriplePending).
		Updates(map[string]interface{}{"status": types.GraphTripleSuperseded, "reviewed_at": now}).Error
}

func (r *graphTripleReviewRepository) MarkSuperseded(ctx context.Context, tenantID uint64, id string) error {
	return r.transitionPending(ctx, tenantID, id, types.GraphTripleSuperseded, "", "configuration or document version changed", false)
}

func (r *graphTripleReviewRepository) transitionPending(
	ctx context.Context,
	tenantID uint64,
	id string,
	to types.GraphTripleReviewStatus,
	reviewerID, comment string,
	written bool,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var candidate types.GraphTripleCandidate
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND id = ?", tenantID, id).
			First(&candidate).Error; err != nil {
			return err
		}
		if err := types.ValidateGraphTripleTransition(candidate.Status, to); err != nil {
			return err
		}
		now := time.Now().UTC()
		updates := map[string]interface{}{
			"status":      to,
			"reviewer_id": reviewerID,
			"comment":     comment,
			"reviewed_at": now,
		}
		if written {
			updates["written_at"] = now
		}
		result := tx.Model(&types.GraphTripleCandidate{}).
			Where("tenant_id = ? AND id = ? AND status = ?", tenantID, id, types.GraphTriplePending).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("graph triple candidate is no longer pending")
		}
		return nil
	})
}
