package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type knowledgeGovernanceRepository struct {
	db           *gorm.DB
	graph        interfaces.RetrieveGraphRepository
	activationMu sync.Mutex
}

// CreateVersionAndSetPending creates a draft version and installs its pending
// pointer in one transaction. Optional knowledge updates are committed with
// the pointer so manual knowledge content cannot be left detached from the
// version after a partial failure.
func (r *knowledgeGovernanceRepository) CreateVersionAndSetPending(
	ctx context.Context,
	version *types.KnowledgeVersion,
	expectedPendingVersionID string,
	knowledgeUpdates map[string]any,
) (bool, error) {
	if version == nil {
		return false, errors.New("knowledge version is required")
	}
	if version.ID == "" {
		version.ID = uuid.NewString()
	}
	updated := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var knowledge struct {
			PendingVersionID *string `gorm:"column:pending_version_id"`
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Table("knowledges").
			Select("pending_version_id").
			Where("tenant_id = ? AND id = ?", version.TenantID, version.KnowledgeID).
			First(&knowledge).Error; err != nil {
			return err
		}
		actualPendingVersionID := ""
		if knowledge.PendingVersionID != nil {
			actualPendingVersionID = strings.TrimSpace(*knowledge.PendingVersionID)
		}
		if actualPendingVersionID != strings.TrimSpace(expectedPendingVersionID) {
			return nil
		}
		if err := tx.Create(version).Error; err != nil {
			return err
		}
		values := make(map[string]any, len(knowledgeUpdates)+1)
		for key, value := range knowledgeUpdates {
			values[key] = value
		}
		values["pending_version_id"] = version.ID
		result := tx.Model(&types.Knowledge{}).
			Where("tenant_id = ? AND id = ?", version.TenantID, version.KnowledgeID).
			Updates(values)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("knowledge not found")
		}
		updated = true
		return nil
	})
	return updated, err
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

func (r *knowledgeGovernanceRepository) PrepareManagedUpload(
	ctx context.Context,
	tenantID uint64,
	knowledgeID, versionID, reviewerID string,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var version types.KnowledgeVersion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND id = ? AND knowledge_id = ?", tenantID, versionID, knowledgeID).
			First(&version).Error; err != nil {
			return err
		}
		for _, next := range []types.KnowledgeVersionStatus{
			types.KnowledgeVersionPendingReview,
			types.KnowledgeVersionApproved,
			types.KnowledgeVersionIndexing,
		} {
			if err := types.TransitionKnowledgeVersion(&version, next); err != nil {
				return err
			}
		}
		if err := tx.Model(&types.KnowledgeVersion{}).
			Where("tenant_id = ? AND id = ?", tenantID, versionID).
			Update("status", types.KnowledgeVersionIndexing).Error; err != nil {
			return err
		}
		if err := tx.Create(&types.KnowledgeVersionReview{
			ID: uuid.NewString(), VersionID: versionID, ReviewerID: reviewerID,
			Action: "auto_approve", Comment: "管理员上传自动批准", CreatedAt: time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		return tx.Model(&types.Knowledge{}).
			Where("tenant_id = ? AND id = ?", tenantID, knowledgeID).
			Update("parse_status", types.ParseStatusPending).Error
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
		var knowledge struct {
			PendingVersionID *string `gorm:"column:pending_version_id"`
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Table("knowledges").Select("pending_version_id").Where("tenant_id = ? AND id = ?", tenantID, version.KnowledgeID).First(&knowledge).Error; err != nil {
			return err
		}
		pendingVersionID := ""
		if knowledge.PendingVersionID != nil {
			pendingVersionID = *knowledge.PendingVersionID
		}
		if pendingVersionID != version.ID {
			return errors.New("knowledge version is no longer pending")
		}
		parseStatus := ""
		switch review.Action {
		case "submit":
			parseStatus = types.ParseStatusPendingReview
		case "withdraw":
			parseStatus = types.ParseStatusDraft
		case "reject":
			parseStatus = types.ParseStatusRejected
		}
		updateKnowledgeStatus := func() error {
			result := tx.Model(&types.Knowledge{}).
				Where("tenant_id = ? AND id = ?", tenantID, version.KnowledgeID).
				Update("parse_status", parseStatus)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("knowledge not found")
			}
			return nil
		}
		// A retried submit is idempotent. Besides avoiding an invalid
		// pending_review -> pending_review transition, this repairs records
		// created before the knowledge status was updated transactionally.
		if review.Action == "submit" && version.Status == status {
			return updateKnowledgeStatus()
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
		if err := tx.Create(review).Error; err != nil {
			return err
		}
		if parseStatus == "" {
			return nil
		}
		return updateKnowledgeStatus()
	})
}

// ActivateVersion switches the current version and retires the previous one
// in one database transaction. A future-effective version remains scheduled
// and does not change the current pointer until ActivateDueVersions runs.
func (r *knowledgeGovernanceRepository) ActivateVersion(ctx context.Context, tenantID uint64, id string, now time.Time) error {
	// Neo4j namespace switching and the SQL pointer update cannot share one
	// transaction. Serialize the cross-store activation on this process while
	// the SQL transaction remains the final source of truth.
	r.activationMu.Lock()
	defer r.activationMu.Unlock()

	candidate, err := r.GetVersion(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if candidate == nil {
		return gorm.ErrRecordNotFound
	}
	var knowledge struct {
		PendingVersionID string `gorm:"column:pending_version_id"`
	}
	if err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Table("knowledges").Select("pending_version_id").Where("tenant_id = ? AND id = ?", tenantID, candidate.KnowledgeID).First(&knowledge).Error; err != nil {
		return err
	}
	if knowledge.PendingVersionID != candidate.ID && candidate.Status != types.KnowledgeVersionSuperseded {
		return fmt.Errorf("knowledge version %q is no longer pending", candidate.ID)
	}
	if r.graph == nil || (candidate.EffectiveAt != nil && now.Before(*candidate.EffectiveAt)) {
		return r.activateVersionDB(ctx, tenantID, id, now)
	}
	var knowledgeBaseID string
	if err := r.db.WithContext(ctx).Table("knowledges").Select("knowledge_base_id").Where("tenant_id = ? AND id = ?", tenantID, candidate.KnowledgeID).Scan(&knowledgeBaseID).Error; err != nil {
		return err
	}
	var kb types.KnowledgeBase
	if err := r.db.WithContext(ctx).Select("id", "indexing_strategy").Where("tenant_id = ? AND id = ?", tenantID, knowledgeBaseID).First(&kb).Error; err != nil {
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
	if err := r.graph.SwitchCanonicalNamespace(ctx, tenantID, knowledgeBaseID, namespace); err != nil {
		return fmt.Errorf("graph namespace is not ready: %w", err)
	}
	if err := r.activateVersionDB(ctx, tenantID, id, now); err != nil {
		if _, rollbackErr := r.graph.RollbackCanonicalNamespace(ctx, tenantID, knowledgeBaseID); rollbackErr != nil {
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
			PendingVersionID *string `gorm:"column:pending_version_id"`
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Table("knowledges").Select("current_version_id, pending_version_id").Where("tenant_id = ? AND id = ?", tenantID, candidate.KnowledgeID).First(&knowledge).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		pendingVersionID := ""
		if knowledge.PendingVersionID != nil {
			pendingVersionID = *knowledge.PendingVersionID
		}
		if pendingVersionID != candidate.ID && candidate.Status != types.KnowledgeVersionSuperseded {
			return fmt.Errorf("knowledge version %q is no longer pending", candidate.ID)
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
			// Keep the expired version ID as a tombstone. An empty current
			// pointer is also the legacy/ungoverned marker, so clearing it would
			// let readers mistake an expired governed document for legacy data.
			return nil
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
