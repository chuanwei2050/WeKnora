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

type acceptanceBenchmarkRepository struct{ db *gorm.DB }

func NewAcceptanceBenchmarkRepository(db *gorm.DB) interfaces.AcceptanceBenchmarkRepository {
	return &acceptanceBenchmarkRepository{db: db}
}

func (r *acceptanceBenchmarkRepository) CreateSuiteVersion(ctx context.Context, suite *types.AcceptanceSuiteVersion) error {
	if suite.ID == "" {
		suite.ID = uuid.NewString()
	}
	if suite.CreatedAt.IsZero() {
		suite.CreatedAt = time.Now().UTC()
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("acceptance_suite_versions").Create(map[string]interface{}{
			"id": suite.ID, "tenant_id": suite.TenantID, "suite_id": suite.SuiteID,
			"version_label": suite.Version, "kind": suite.Kind,
			"routing_taxonomy_id": suite.RoutingTaxonomyID, "routing_taxonomy_version": suite.RoutingTaxonomyVersion,
			"frozen": suite.Frozen, "created_at": suite.CreatedAt, "frozen_at": suite.FrozenAt,
		}).Error; err != nil {
			return err
		}
		for index, item := range suite.Cases {
			caseID := item.ID
			if caseID == "" {
				caseID = uuid.NewString()
			}
			suite.Cases[index].ID = caseID
			payload, err := json.Marshal(item)
			if err != nil {
				return err
			}
			if err := tx.Table("acceptance_cases").Create(map[string]interface{}{"id": caseID, "suite_version_id": suite.ID, "payload": payload}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

type acceptanceSuiteRow struct {
	ID                     string
	TenantID               uint64
	SuiteID                string
	Version                string `gorm:"column:version_label"`
	Kind                   types.AcceptanceSuiteKind
	RoutingTaxonomyID      string
	RoutingTaxonomyVersion string
	Frozen                 bool
	CreatedAt              time.Time
	FrozenAt               *time.Time
}

func (r *acceptanceBenchmarkRepository) GetSuiteVersion(ctx context.Context, tenantID uint64, id string) (*types.AcceptanceSuiteVersion, error) {
	var row acceptanceSuiteRow
	if err := r.db.WithContext(ctx).Table("acceptance_suite_versions").Where("tenant_id = ? AND id = ?", tenantID, id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	suite := &types.AcceptanceSuiteVersion{ID: row.ID, TenantID: row.TenantID, SuiteID: row.SuiteID, Version: row.Version, Kind: row.Kind, RoutingTaxonomyID: row.RoutingTaxonomyID, RoutingTaxonomyVersion: row.RoutingTaxonomyVersion, Frozen: row.Frozen, CreatedAt: row.CreatedAt, FrozenAt: row.FrozenAt}
	var cases []struct {
		ID      string
		Payload []byte
	}
	if err := r.db.WithContext(ctx).Table("acceptance_cases").Select("id, payload").Where("suite_version_id = ?", id).Find(&cases).Error; err != nil {
		return nil, err
	}
	for _, item := range cases {
		var acceptanceCase types.AcceptanceCase
		if err := json.Unmarshal(item.Payload, &acceptanceCase); err != nil {
			return nil, err
		}
		acceptanceCase.ID = item.ID
		suite.Cases = append(suite.Cases, acceptanceCase)
	}
	return suite, nil
}

func (r *acceptanceBenchmarkRepository) ListSuiteVersions(ctx context.Context, tenantID uint64, suiteID string) ([]*types.AcceptanceSuiteVersion, error) {
	var rows []acceptanceSuiteRow
	query := r.db.WithContext(ctx).Table("acceptance_suite_versions").Where("tenant_id = ?", tenantID)
	if suiteID != "" {
		query = query.Where("suite_id = ?", suiteID)
	}
	if err := query.Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]*types.AcceptanceSuiteVersion, 0, len(rows))
	for _, row := range rows {
		suite, err := r.GetSuiteVersion(ctx, tenantID, row.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, suite)
	}
	return result, nil
}

func (r *acceptanceBenchmarkRepository) FreezeSuiteVersion(ctx context.Context, tenantID uint64, id string, frozenAt time.Time) error {
	return r.db.WithContext(ctx).Table("acceptance_suite_versions").Where("tenant_id = ? AND id = ? AND frozen = FALSE", tenantID, id).
		Updates(map[string]interface{}{"frozen": true, "frozen_at": frozenAt}).Error
}

func (r *acceptanceBenchmarkRepository) CreateRun(ctx context.Context, run *types.AcceptanceRun) error {
	if run.ID == "" {
		run.ID = uuid.NewString()
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}
	snapshot, err := json.Marshal(run.Snapshot)
	if err != nil {
		return err
	}
	metrics, err := json.Marshal(run.Metrics)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Table("acceptance_runs").Create(map[string]interface{}{
		"id": run.ID, "tenant_id": run.TenantID, "suite_version_id": run.SuiteVersionID,
		"profile": run.Profile, "snapshot": snapshot, "metrics": metrics,
		"gate": run.Gate, "created_at": run.CreatedAt,
	}).Error
}

func (r *acceptanceBenchmarkRepository) UpdateRun(ctx context.Context, tenantID uint64, run *types.AcceptanceRun) error {
	if run == nil || run.ID == "" {
		return errors.New("acceptance run is required")
	}
	metrics, err := json.Marshal(run.Metrics)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Table("acceptance_runs").
		Where("tenant_id = ? AND id = ?", tenantID, run.ID).
		Updates(map[string]interface{}{"metrics": metrics, "gate": run.Gate}).Error
}

func (r *acceptanceBenchmarkRepository) GetRun(ctx context.Context, tenantID uint64, id string) (*types.AcceptanceRun, error) {
	var row struct {
		ID             string
		TenantID       uint64
		SuiteVersionID string
		Profile        types.AcceptanceProfile
		Snapshot       []byte
		Metrics        []byte
		Gate           types.AcceptanceGateStatus
		CreatedAt      time.Time
	}
	if err := r.db.WithContext(ctx).Table("acceptance_runs").Where("tenant_id = ? AND id = ?", tenantID, id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var snapshot types.AcceptanceRunSnapshot
	if err := json.Unmarshal(row.Snapshot, &snapshot); err != nil {
		return nil, err
	}
	var metrics types.AcceptanceMetrics
	if len(row.Metrics) > 0 {
		if err := json.Unmarshal(row.Metrics, &metrics); err != nil {
			return nil, err
		}
	}
	return &types.AcceptanceRun{ID: row.ID, TenantID: row.TenantID, SuiteVersionID: row.SuiteVersionID, Profile: row.Profile, Snapshot: snapshot, Metrics: metrics, Gate: row.Gate, CreatedAt: row.CreatedAt}, nil
}

func (r *acceptanceBenchmarkRepository) ListRuns(ctx context.Context, tenantID uint64, suiteVersionID string) ([]*types.AcceptanceRun, error) {
	var ids []string
	query := r.db.WithContext(ctx).Table("acceptance_runs").Where("tenant_id = ?", tenantID)
	if suiteVersionID != "" {
		query = query.Where("suite_version_id = ?", suiteVersionID)
	}
	if err := query.Order("created_at DESC").Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	runs := make([]*types.AcceptanceRun, 0, len(ids))
	for _, id := range ids {
		run, err := r.GetRun(ctx, tenantID, id)
		if err != nil {
			return nil, err
		}
		if run != nil {
			runs = append(runs, run)
		}
	}
	return runs, nil
}

func (r *acceptanceBenchmarkRepository) CreateCaseResult(ctx context.Context, result *types.AcceptanceCaseResultRecord) error {
	if result.ID == "" {
		result.ID = uuid.NewString()
	}
	payload, err := json.Marshal(result.Payload)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Table("acceptance_case_results").Create(map[string]interface{}{
		"id": result.ID, "run_id": result.RunID, "case_id": result.CaseID, "payload": payload,
	}).Error
}

func (r *acceptanceBenchmarkRepository) ListCaseResults(ctx context.Context, tenantID uint64, runID string) ([]types.AcceptanceCaseResultRecord, error) {
	var rows []struct {
		ID      string
		RunID   string
		CaseID  string
		Payload []byte
	}
	if err := r.db.WithContext(ctx).Table("acceptance_case_results AS c").
		Select("c.id, c.run_id, c.case_id, c.payload").
		Joins("JOIN acceptance_runs AS r ON r.id = c.run_id").
		Where("r.tenant_id = ? AND c.run_id = ?", tenantID, runID).
		Order("c.id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	results := make([]types.AcceptanceCaseResultRecord, 0, len(rows))
	for _, row := range rows {
		var payload types.AcceptanceCaseResult
		if err := json.Unmarshal(row.Payload, &payload); err != nil {
			return nil, err
		}
		results = append(results, types.AcceptanceCaseResultRecord{ID: row.ID, RunID: row.RunID, CaseID: row.CaseID, Payload: payload})
	}
	return results, nil
}

func (r *acceptanceBenchmarkRepository) GetCaseResult(ctx context.Context, tenantID uint64, runID, caseID string) (*types.AcceptanceCaseResultRecord, error) {
	var row struct {
		ID      string
		RunID   string
		CaseID  string
		Payload []byte
	}
	if err := r.db.WithContext(ctx).Table("acceptance_case_results AS c").
		Select("c.id, c.run_id, c.case_id, c.payload").
		Joins("JOIN acceptance_runs AS r ON r.id = c.run_id").
		Where("r.tenant_id = ? AND c.run_id = ? AND c.case_id = ?", tenantID, runID, caseID).
		First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var payload types.AcceptanceCaseResult
	if err := json.Unmarshal(row.Payload, &payload); err != nil {
		return nil, err
	}
	return &types.AcceptanceCaseResultRecord{ID: row.ID, RunID: row.RunID, CaseID: row.CaseID, Payload: payload}, nil
}

func (r *acceptanceBenchmarkRepository) UpdateCaseResult(ctx context.Context, tenantID uint64, result *types.AcceptanceCaseResultRecord) error {
	if result == nil {
		return errors.New("acceptance case result is required")
	}
	payload, err := json.Marshal(result.Payload)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Table("acceptance_case_results").
		Where("id = ? AND run_id IN (SELECT id FROM acceptance_runs WHERE tenant_id = ?)", result.ID, tenantID).
		Update("payload", payload).Error
}

func (r *acceptanceBenchmarkRepository) CreateArtifact(ctx context.Context, artifact *types.AcceptanceArtifact) error {
	if artifact == nil {
		return errors.New("acceptance artifact is required")
	}
	if err := artifact.Validate(); err != nil {
		return err
	}
	if artifact.ID == "" {
		artifact.ID = uuid.NewString()
	}
	if artifact.CreatedAt.IsZero() {
		artifact.CreatedAt = time.Now().UTC()
	}
	return r.db.WithContext(ctx).Table("acceptance_artifacts").Create(map[string]interface{}{
		"id": artifact.ID, "run_id": artifact.RunID, "kind": artifact.Kind, "uri": artifact.URI,
		"sha256": artifact.SHA256, "size_bytes": artifact.Size,
		"content_type": artifact.ContentType, "created_at": artifact.CreatedAt,
	}).Error
}

func (r *acceptanceBenchmarkRepository) ListArtifacts(ctx context.Context, tenantID uint64, runID string) ([]types.AcceptanceArtifact, error) {
	var rows []types.AcceptanceArtifact
	query := r.db.WithContext(ctx).Table("acceptance_artifacts AS a").
		Select("a.id, a.run_id, a.kind, a.uri, a.sha256, a.size_bytes AS size, a.content_type, a.created_at").
		Joins("JOIN acceptance_runs AS r ON r.id = a.run_id").
		Where("r.tenant_id = ? AND a.run_id = ?", tenantID, runID).
		Order("a.created_at ASC")
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
