package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// feishuPublishRepository persists Feishu publish configs, snapshots, runs and mappings.
type feishuPublishRepository struct {
	db *gorm.DB
}

// NewFeishuPublishRepository creates a tenant-scoped Feishu publish repository.
func NewFeishuPublishRepository(db *gorm.DB) interfaces.FeishuPublishRepository {
	return &feishuPublishRepository{db: db}
}

func (r *feishuPublishRepository) GetConfigByKB(ctx context.Context, tenantID uint64, kbID string) (*types.FeishuPublishConfig, error) {
	if kbID == "" {
		return nil, errors.New("knowledge base id is empty")
	}
	var cfg types.FeishuPublishConfig
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID).
		First(&cfg).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cfg.AppSecretConfigured = strings.TrimSpace(cfg.AppSecretCipher) != ""
	return &cfg, nil
}

func (r *feishuPublishRepository) UpsertConfig(ctx context.Context, cfg *types.FeishuPublishConfig) error {
	if cfg == nil {
		return errors.New("config is nil")
	}
	if cfg.TenantID == 0 || cfg.KnowledgeBaseID == "" {
		return errors.New("tenant_id and knowledge_base_id are required")
	}

	existing, err := r.GetConfigByKB(ctx, cfg.TenantID, cfg.KnowledgeBaseID)
	if err != nil {
		return err
	}
	if existing != nil {
		cfg.ID = existing.ID
		cfg.CreatedAt = existing.CreatedAt
		return r.db.WithContext(ctx).Model(&types.FeishuPublishConfig{}).
			Where("tenant_id = ? AND id = ?", cfg.TenantID, cfg.ID).
			Select("*").
			Updates(cfg).Error
	}
	if cfg.ID == "" {
		cfg.ID = uuid.NewString()
	}
	return r.db.WithContext(ctx).Create(cfg).Error
}

func (r *feishuPublishRepository) SoftDeleteConfig(ctx context.Context, tenantID uint64, kbID string) error {
	if kbID == "" {
		return errors.New("knowledge base id is empty")
	}
	res := r.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID).
		Delete(&types.FeishuPublishConfig{})
	return res.Error
}

func (r *feishuPublishRepository) CreateSnapshot(ctx context.Context, snap *types.FeishuPublishSnapshot) error {
	if snap == nil {
		return errors.New("snapshot is nil")
	}
	if snap.TenantID == 0 || snap.KnowledgeBaseID == "" {
		return errors.New("tenant_id and knowledge_base_id are required")
	}
	if strings.TrimSpace(snap.TargetID) == "" || strings.TrimSpace(snap.Digest) == "" {
		return errors.New("target_id and digest are required")
	}
	if snap.ID == "" {
		snap.ID = uuid.NewString()
	}
	err := r.db.WithContext(ctx).Create(snap).Error
	if err == nil {
		return nil
	}
	if !isUniqueConstraintError(err) {
		return err
	}
	existing, getErr := r.GetSnapshotByDigest(ctx, snap.TenantID, snap.KnowledgeBaseID, snap.TargetID, snap.Digest)
	if getErr != nil {
		return getErr
	}
	if existing == nil {
		return err
	}
	*snap = *existing
	return nil
}

func (r *feishuPublishRepository) GetSnapshot(ctx context.Context, tenantID uint64, id string) (*types.FeishuPublishSnapshot, error) {
	if id == "" {
		return nil, errors.New("snapshot id is empty")
	}
	var snap types.FeishuPublishSnapshot
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		First(&snap).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &snap, nil
}

func (r *feishuPublishRepository) GetSnapshotByDigest(ctx context.Context, tenantID uint64, kbID, targetID, digest string) (*types.FeishuPublishSnapshot, error) {
	if kbID == "" || targetID == "" || digest == "" {
		return nil, errors.New("kb_id, target_id and digest are required")
	}
	var snap types.FeishuPublishSnapshot
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_base_id = ? AND target_id = ? AND digest = ?",
			tenantID, kbID, targetID, digest).
		First(&snap).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &snap, nil
}

func (r *feishuPublishRepository) CreateRun(ctx context.Context, run *types.FeishuPublishRun) error {
	if run == nil {
		return errors.New("run is nil")
	}
	if run.TenantID == 0 || run.KnowledgeBaseID == "" || run.TargetID == "" || run.SnapshotDigest == "" {
		return errors.New("tenant_id, knowledge_base_id, target_id and snapshot_digest are required")
	}
	if run.ID == "" {
		run.ID = uuid.NewString()
	}
	err := r.db.WithContext(ctx).Create(run).Error
	if err == nil {
		return nil
	}
	if !isUniqueConstraintError(err) {
		return err
	}
	existing, getErr := r.GetRunByDigest(ctx, run.TenantID, run.KnowledgeBaseID, run.TargetID, run.SnapshotDigest)
	if getErr != nil {
		return getErr
	}
	if existing != nil {
		*run = *existing
		return nil
	}
	active, activeErr := r.GetActiveRun(ctx, run.TenantID, run.KnowledgeBaseID, run.TargetID)
	if activeErr != nil {
		return activeErr
	}
	if active != nil {
		if active.SnapshotDigest == run.SnapshotDigest {
			*run = *active
			return nil
		}
		return types.ErrFeishuPublishActiveRunExists
	}
	return err
}

func (r *feishuPublishRepository) UpdateRun(ctx context.Context, run *types.FeishuPublishRun) error {
	if run == nil {
		return errors.New("run is nil")
	}
	if run.ID == "" || run.TenantID == 0 {
		return errors.New("run id and tenant_id are required")
	}
	return r.db.WithContext(ctx).Model(&types.FeishuPublishRun{}).
		Where("tenant_id = ? AND id = ?", run.TenantID, run.ID).
		Select("*").
		Updates(run).Error
}

// ClaimRun atomically transitions a queued run to running. Returns false if another worker claimed it
// or the run is no longer queued.
func (r *feishuPublishRepository) ClaimRun(ctx context.Context, tenantID uint64, runID string) (bool, error) {
	if runID == "" || tenantID == 0 {
		return false, errors.New("run id and tenant_id are required")
	}
	now := time.Now().UTC()
	res := r.db.WithContext(ctx).Model(&types.FeishuPublishRun{}).
		Where("tenant_id = ? AND id = ? AND status = ?", tenantID, runID, types.FeishuPublishRunQueued).
		Updates(map[string]interface{}{
			"status":         types.FeishuPublishRunRunning,
			"stage":          "initializing",
			"progress_done":  0,
			"progress_total": 0,
			"progress_label": "",
			"started_at":     now,
			"updated_at":     now,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *feishuPublishRepository) GetRun(ctx context.Context, tenantID uint64, id string) (*types.FeishuPublishRun, error) {
	if id == "" {
		return nil, errors.New("run id is empty")
	}
	var run types.FeishuPublishRun
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *feishuPublishRepository) GetRunByDigest(ctx context.Context, tenantID uint64, kbID, targetID, digest string) (*types.FeishuPublishRun, error) {
	if kbID == "" || targetID == "" || digest == "" {
		return nil, errors.New("kb_id, target_id and digest are required")
	}
	var run types.FeishuPublishRun
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_base_id = ? AND target_id = ? AND snapshot_digest = ?",
			tenantID, kbID, targetID, digest).
		First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *feishuPublishRepository) GetActiveRun(ctx context.Context, tenantID uint64, kbID, targetID string) (*types.FeishuPublishRun, error) {
	if kbID == "" || targetID == "" {
		return nil, errors.New("kb_id and target_id are required")
	}
	var run types.FeishuPublishRun
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_base_id = ? AND target_id = ?", tenantID, kbID, targetID).
		Where("status IN ?", []string{types.FeishuPublishRunQueued, types.FeishuPublishRunRunning}).
		Order("created_at ASC").
		First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *feishuPublishRepository) ListRunsByKB(ctx context.Context, tenantID uint64, kbID string, limit int) ([]*types.FeishuPublishRun, error) {
	if kbID == "" {
		return nil, errors.New("knowledge base id is empty")
	}
	if limit <= 0 {
		limit = 20
	}
	var runs []*types.FeishuPublishRun
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID).
		Order("created_at DESC").
		Limit(limit).
		Find(&runs).Error
	if err != nil {
		return nil, err
	}
	return runs, nil
}

func (r *feishuPublishRepository) ListMappings(ctx context.Context, tenantID uint64, kbID, targetID string) ([]*types.FeishuPublishMapping, error) {
	if kbID == "" || targetID == "" {
		return nil, errors.New("kb_id and target_id are required")
	}
	var mappings []*types.FeishuPublishMapping
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_base_id = ? AND target_id = ?", tenantID, kbID, targetID).
		Find(&mappings).Error
	if err != nil {
		return nil, err
	}
	return mappings, nil
}

func (r *feishuPublishRepository) GetMapping(ctx context.Context, tenantID uint64, kbID, targetID, sourceKind, sourceID string) (*types.FeishuPublishMapping, error) {
	if kbID == "" || targetID == "" || sourceKind == "" || sourceID == "" {
		return nil, errors.New("kb_id, target_id, source_kind and source_id are required")
	}
	var m types.FeishuPublishMapping
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_base_id = ? AND target_id = ? AND source_kind = ? AND source_id = ?",
			tenantID, kbID, targetID, sourceKind, sourceID).
		First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *feishuPublishRepository) UpsertMapping(ctx context.Context, m *types.FeishuPublishMapping) error {
	if m == nil {
		return errors.New("mapping is nil")
	}
	if m.TenantID == 0 || m.KnowledgeBaseID == "" || m.TargetID == "" || m.SourceKind == "" || m.SourceID == "" {
		return errors.New("tenant_id, knowledge_base_id, target_id, source_kind and source_id are required")
	}
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "tenant_id"},
			{Name: "knowledge_base_id"},
			{Name: "target_id"},
			{Name: "source_kind"},
			{Name: "source_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"space_id", "node_token", "obj_token", "parent_node_token", "title",
			"meta_block_ids", "body_block_ids", "file_block_id", "media_token",
			"source_hash", "body_hash", "source_version", "status", "last_success_run_id", "updated_at",
		}),
	}).Create(m).Error
}

func (r *feishuPublishRepository) DeleteMappingsByTarget(ctx context.Context, tenantID uint64, kbID, targetID string) error {
	if kbID == "" || targetID == "" {
		return errors.New("kb_id and target_id are required")
	}
	return r.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_base_id = ? AND target_id = ?", tenantID, kbID, targetID).
		Delete(&types.FeishuPublishMapping{}).Error
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")
}
