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

type knowledgeGovernanceRepository struct {
	db    *gorm.DB
	graph interfaces.RetrieveGraphRepository
}

func NewKnowledgeGovernanceRepository(db *gorm.DB, graph interfaces.RetrieveGraphRepository) interfaces.KnowledgeGovernanceRepository {
	return &knowledgeGovernanceRepository{db: db, graph: graph}
}

func (r *knowledgeGovernanceRepository) CreateVersion(ctx context.Context, version *types.KnowledgeVersion) error {
	if version.ID == "" {
		version.ID = uuid.NewString()
	}
	return r.db.WithContext(ctx).Create(version).Error
}

func (r *knowledgeGovernanceRepository) DeleteDraftVersion(ctx context.Context, tenantID uint64, id string) error {
	return r.db.WithContext(ctx).
		Where("tenant_id = ? AND id = ? AND status = ?", tenantID, id, types.KnowledgeVersionDraft).
		Delete(&types.KnowledgeVersion{}).Error
}

func (r *knowledgeGovernanceRepository) GetVersion(ctx context.Context, tenantID uint64, id string) (*types.KnowledgeVersion, error) {
	var version types.KnowledgeVersion
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).First(&version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &version, err
}

func (r *knowledgeGovernanceRepository) ListVersions(ctx context.Context, tenantID uint64, knowledgeID string) ([]*types.KnowledgeVersion, error) {
	var versions []*types.KnowledgeVersion
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_id = ?", tenantID, knowledgeID).
		Order("created_at DESC").Find(&versions).Error
	return versions, err
}

func (r *knowledgeGovernanceRepository) UpdateVersionStatus(ctx context.Context, tenantID uint64, id string, status types.KnowledgeVersionStatus) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var version types.KnowledgeVersion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, id).First(&version).Error; err != nil {
			return err
		}
		if err := types.TransitionKnowledgeVersion(&version, status); err != nil {
			return err
		}
		return tx.Model(&types.KnowledgeVersion{}).Where("tenant_id = ? AND id = ?", tenantID, id).Update("status", status).Error
	})
}

func (r *knowledgeGovernanceRepository) TransitionVersionWithReview(
	ctx context.Context,
	tenantID uint64,
	id string,
	status types.KnowledgeVersionStatus,
	review *types.KnowledgeVersionReview,
) error {
	if review == nil {
		return errors.New("knowledge version review is required")
	}
	if review.ID == "" {
		review.ID = uuid.NewString()
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var version types.KnowledgeVersion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, id).First(&version).Error; err != nil {
			return err
		}
		if review.VersionID != version.ID {
			return errors.New("knowledge version review does not match version")
		}
		if version.Status == types.KnowledgeVersionRejected && status == types.KnowledgeVersionPendingReview {
			if err := types.TransitionKnowledgeVersion(&version, types.KnowledgeVersionDraft); err != nil {
				return err
			}
		}
		if err := types.TransitionKnowledgeVersion(&version, status); err != nil {
			return err
		}
		if err := tx.Model(&types.KnowledgeVersion{}).Where("tenant_id = ? AND id = ?", tenantID, id).Update("status", status).Error; err != nil {
			return err
		}
		return tx.Create(review).Error
	})
}

// ActivateVersion switches the current version and retires the previous one
// in one database transaction. A future-effective version remains scheduled
// and does not change the current pointer until ActivateDueVersions runs.
func (r *knowledgeGovernanceRepository) ActivateVersion(ctx context.Context, tenantID uint64, id string, now time.Time) error {
	candidate, err := r.GetVersion(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if candidate == nil {
		return gorm.ErrRecordNotFound
	}
	if r.graph == nil || (candidate.EffectiveAt != nil && now.Before(*candidate.EffectiveAt)) {
		return r.activateVersionDB(ctx, tenantID, id, now)
	}
	var knowledge struct {
		KnowledgeBaseID string `gorm:"column:knowledge_base_id"`
	}
	if err := r.db.WithContext(ctx).Table("knowledges").Select("knowledge_base_id").Where("tenant_id = ? AND id = ?", tenantID, candidate.KnowledgeID).First(&knowledge).Error; err != nil {
		return err
	}
	var kb types.KnowledgeBase
	if err := r.db.WithContext(ctx).Select("id", "indexing_strategy").Where("tenant_id = ? AND id = ?", tenantID, knowledge.KnowledgeBaseID).First(&kb).Error; err != nil {
		return err
	}
	kb.EnsureDefaults()
	if !kb.IsGraphEnabled() {
		return r.activateVersionDB(ctx, tenantID, id, now)
	}
	var chunks []types.Chunk
	if err := r.db.WithContext(ctx).Select("content").Where("tenant_id = ? AND knowledge_version_id = ?", tenantID, candidate.ID).Find(&chunks).Error; err != nil {
		return err
	}
	hasGraphCandidate := false
	for _, chunk := range chunks {
		if types.NeedsEntityRelation(chunk.Content) {
			hasGraphCandidate = true
			break
		}
	}
	if !hasGraphCandidate {
		return r.activateVersionDB(ctx, tenantID, id, now)
	}
	namespace := types.GraphNamespaceForVersion(candidate.ID, true)
	if err := r.graph.SwitchCanonicalNamespace(ctx, tenantID, knowledge.KnowledgeBaseID, namespace); err != nil {
		return fmt.Errorf("graph namespace is not ready: %w", err)
	}
	if err := r.activateVersionDB(ctx, tenantID, id, now); err != nil {
		if _, rollbackErr := r.graph.RollbackCanonicalNamespace(ctx, tenantID, knowledge.KnowledgeBaseID); rollbackErr != nil {
			return fmt.Errorf("activate version: %v; graph rollback: %w", err, rollbackErr)
		}
		return err
	}
	return nil
}

func (r *knowledgeGovernanceRepository) activateVersionDB(ctx context.Context, tenantID uint64, id string, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var candidate types.KnowledgeVersion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, id).First(&candidate).Error; err != nil {
			return err
		}
		switch candidate.Status {
		case types.KnowledgeVersionApproved:
			// Publishing always records the indexing phase, even when the index
			// was prepared by an external worker and the switch is immediate.
			if err := tx.Model(&candidate).Update("status", types.KnowledgeVersionIndexing).Error; err != nil {
				return err
			}
			candidate.Status = types.KnowledgeVersionIndexing
		case types.KnowledgeVersionIndexing, types.KnowledgeVersionPublishFailed, types.KnowledgeVersionSuperseded, types.KnowledgeVersionScheduled:
		default:
			return fmt.Errorf("knowledge version %q cannot be activated from %s", candidate.ID, candidate.Status)
		}
		if candidate.EffectiveAt != nil && now.Before(*candidate.EffectiveAt) {
			if candidate.Status != types.KnowledgeVersionScheduled {
				if err := types.TransitionKnowledgeVersion(&candidate, types.KnowledgeVersionScheduled); err != nil {
					return err
				}
			}
			if err := tx.Model(&candidate).Update("status", types.KnowledgeVersionScheduled).Error; err != nil {
				return err
			}
			return nil
		}
		if candidate.ExpiresAt != nil && !now.Before(*candidate.ExpiresAt) {
			return fmt.Errorf("knowledge version %q is already expired", candidate.ID)
		}
		var knowledge struct {
			CurrentVersionID *string `gorm:"column:current_version_id"`
		}
		if err := tx.Table("knowledges").Select("current_version_id").Where("tenant_id = ? AND id = ?", tenantID, candidate.KnowledgeID).First(&knowledge).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Model(&types.KnowledgeVersion{}).
			Where("tenant_id = ? AND knowledge_id = ? AND status = ? AND id <> ?", tenantID, candidate.KnowledgeID, types.KnowledgeVersionActive, candidate.ID).
			Update("status", types.KnowledgeVersionSuperseded).Error; err != nil {
			return err
		}
		if knowledge.CurrentVersionID != nil && *knowledge.CurrentVersionID != "" && *knowledge.CurrentVersionID != candidate.ID {
			candidate.PreviousVersionID = *knowledge.CurrentVersionID
		}
		if err := tx.Model(&candidate).Updates(map[string]any{"status": types.KnowledgeVersionActive, "previous_version_id": candidate.PreviousVersionID}).Error; err != nil {
			return err
		}
		return tx.Table("knowledges").Where("tenant_id = ? AND id = ?", tenantID, candidate.KnowledgeID).
			Updates(map[string]any{"current_version_id": candidate.ID, "pending_version_id": ""}).Error
	})
}

func (r *knowledgeGovernanceRepository) ActivateDueVersions(ctx context.Context, now time.Time) (int, error) {
	var ids []string
	if err := r.db.WithContext(ctx).Model(&types.KnowledgeVersion{}).
		Where("status = ? AND effective_at IS NOT NULL AND effective_at <= ?", types.KnowledgeVersionScheduled, now).
		Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	activated := 0
	for _, id := range ids {
		var version types.KnowledgeVersion
		if err := r.db.WithContext(ctx).Where("id = ?", id).First(&version).Error; err != nil {
			return activated, err
		}
		if err := r.ActivateVersion(ctx, version.TenantID, id, now); err != nil {
			return activated, err
		}
		activated++
	}
	var expired []types.KnowledgeVersion
	if err := r.db.WithContext(ctx).
		Where("status = ? AND expires_at IS NOT NULL AND expires_at <= ?", types.KnowledgeVersionActive, now).
		Find(&expired).Error; err != nil {
		return activated, err
	}
	for _, version := range expired {
		err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var current types.KnowledgeVersion
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", version.TenantID, version.ID).First(&current).Error; err != nil {
				return err
			}
			if current.Status != types.KnowledgeVersionActive || current.ExpiresAt == nil || now.Before(*current.ExpiresAt) {
				return nil
			}
			if err := tx.Model(&current).Update("status", types.KnowledgeVersionExpired).Error; err != nil {
				return err
			}
			return tx.Table("knowledges").Where("tenant_id = ? AND id = ? AND current_version_id = ?", current.TenantID, current.KnowledgeID, current.ID).
				Update("current_version_id", nil).Error
		})
		if err != nil {
			return activated, err
		}
		activated++
	}
	return activated, nil
}

func (r *knowledgeGovernanceRepository) CreateReview(ctx context.Context, review *types.KnowledgeVersionReview) error {
	if review.ID == "" {
		review.ID = uuid.NewString()
	}
	return r.db.WithContext(ctx).Create(review).Error
}

func (r *knowledgeGovernanceRepository) ListReviews(ctx context.Context, versionID string) ([]*types.KnowledgeVersionReview, error) {
	var reviews []*types.KnowledgeVersionReview
	err := r.db.WithContext(ctx).Where("version_id = ?", versionID).Order("created_at ASC").Find(&reviews).Error
	return reviews, err
}
