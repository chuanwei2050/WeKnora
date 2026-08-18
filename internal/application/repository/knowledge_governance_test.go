package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openKnowledgeGovernanceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE knowledge_versions (
			id TEXT PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			knowledge_id TEXT NOT NULL,
			version_label TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			snapshot_ref TEXT,
			source_metadata TEXT NOT NULL DEFAULT '{}',
			previous_version_id TEXT,
			status TEXT NOT NULL,
			created_by TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			effective_at DATETIME,
			expires_at DATETIME
		)`,
		`CREATE TABLE knowledges (
			id TEXT PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			current_version_id TEXT
		)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func newGovernedTestVersion(tenantID uint64, knowledgeID, status string, effectiveAt, expiresAt *time.Time) *types.KnowledgeVersion {
	return &types.KnowledgeVersion{
		ID: uuid.NewString(), TenantID: tenantID, KnowledgeID: knowledgeID,
		VersionLabel: uuid.NewString(), ContentHash: types.HashKnowledgeContent([]byte(uuid.NewString())),
		SourceMetadata: types.KnowledgeSourceMetadata{
			Layer: types.KnowledgeLayerFoundation, SourceCategory: "test", AuthorityLevel: "test",
		},
		Status: types.KnowledgeVersionStatus(status), CreatedBy: "test", CreatedAt: time.Now().UTC(),
		EffectiveAt: effectiveAt, ExpiresAt: expiresAt,
	}
}

func TestDeleteDraftVersionDoesNotDeletePublishedVersion(t *testing.T) {
	db := openKnowledgeGovernanceTestDB(t)
	repo := NewKnowledgeGovernanceRepository(db, nil)
	ctx := context.Background()
	draft := newGovernedTestVersion(1, "knowledge-1", string(types.KnowledgeVersionDraft), nil, nil)
	active := newGovernedTestVersion(1, "knowledge-1", string(types.KnowledgeVersionActive), nil, nil)
	if err := repo.CreateVersion(ctx, draft); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateVersion(ctx, active); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteDraftVersion(ctx, 1, draft.ID); err != nil {
		t.Fatal(err)
	}
	if stored, err := repo.GetVersion(ctx, 1, draft.ID); err != nil || stored != nil {
		t.Fatalf("draft cleanup = %#v, err=%v", stored, err)
	}
	if err := repo.DeleteDraftVersion(ctx, 1, active.ID); err != nil {
		t.Fatal(err)
	}
	if stored, err := repo.GetVersion(ctx, 1, active.ID); err != nil || stored == nil {
		t.Fatalf("active version must remain: %#v, err=%v", stored, err)
	}
}

func TestKnowledgeGovernanceActivationKeepsFutureVersionScheduled(t *testing.T) {
	db := openKnowledgeGovernanceTestDB(t)
	repo := NewKnowledgeGovernanceRepository(db, nil)
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	current := newGovernedTestVersion(1, "knowledge-1", string(types.KnowledgeVersionActive), nil, nil)
	candidateTime := now.Add(time.Hour)
	candidate := newGovernedTestVersion(1, "knowledge-1", string(types.KnowledgeVersionIndexing), &candidateTime, nil)
	if err := repo.CreateVersion(ctx, current); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateVersion(ctx, candidate); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO knowledges (id, tenant_id, current_version_id) VALUES (?, ?, ?)", "knowledge-1", 1, current.ID).Error; err != nil {
		t.Fatal(err)
	}

	if err := repo.ActivateVersion(ctx, 1, candidate.ID, now); err != nil {
		t.Fatal(err)
	}
	storedCandidate, err := repo.GetVersion(ctx, 1, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedCandidate.Status != types.KnowledgeVersionScheduled {
		t.Fatalf("future version status = %s, want scheduled", storedCandidate.Status)
	}
	var knowledge struct {
		CurrentVersionID *string `gorm:"column:current_version_id"`
	}
	if err := db.Table("knowledges").Where("id = ?", "knowledge-1").First(&knowledge).Error; err != nil {
		t.Fatal(err)
	}
	if knowledge.CurrentVersionID == nil || *knowledge.CurrentVersionID != current.ID {
		t.Fatalf("current version changed before effective time: %+v", knowledge.CurrentVersionID)
	}

	activated, err := repo.ActivateDueVersions(ctx, candidateTime)
	if err != nil {
		t.Fatal(err)
	}
	if activated != 1 {
		t.Fatalf("activated due versions = %d, want 1", activated)
	}
	storedCandidate, err = repo.GetVersion(ctx, 1, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedCandidate.Status != types.KnowledgeVersionActive || !storedCandidate.IsRetrievable(candidateTime) {
		t.Fatalf("due version was not activated/retrievable: %+v", storedCandidate)
	}
	oldCurrent, err := repo.GetVersion(ctx, 1, current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if oldCurrent.Status != types.KnowledgeVersionSuperseded {
		t.Fatalf("old current status = %s, want superseded", oldCurrent.Status)
	}
}

func TestKnowledgeGovernanceActivationSupportsRetryAndRollback(t *testing.T) {
	db := openKnowledgeGovernanceTestDB(t)
	repo := NewKnowledgeGovernanceRepository(db, nil)
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	current := newGovernedTestVersion(1, "knowledge-2", string(types.KnowledgeVersionActive), nil, nil)
	failed := newGovernedTestVersion(1, "knowledge-2", string(types.KnowledgeVersionPublishFailed), nil, nil)
	if err := repo.CreateVersion(ctx, current); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateVersion(ctx, failed); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO knowledges (id, tenant_id, current_version_id) VALUES (?, ?, ?)", "knowledge-2", 1, current.ID).Error; err != nil {
		t.Fatal(err)
	}

	if err := repo.UpdateVersionStatus(ctx, 1, failed.ID, types.KnowledgeVersionIndexing); err != nil {
		t.Fatal(err)
	}
	if err := repo.ActivateVersion(ctx, 1, failed.ID, now); err != nil {
		t.Fatal(err)
	}
	failed, err := repo.GetVersion(ctx, 1, failed.ID)
	if err != nil || failed.Status != types.KnowledgeVersionActive {
		t.Fatalf("retry did not activate failed version: version=%+v err=%v", failed, err)
	}
	if err := repo.ActivateVersion(ctx, 1, current.ID, now); err != nil {
		t.Fatal(err)
	}
	current, err = repo.GetVersion(ctx, 1, current.ID)
	if err != nil || current.Status != types.KnowledgeVersionActive {
		t.Fatalf("rollback did not reactivate superseded version: version=%+v err=%v", current, err)
	}
	if current.PreviousVersionID != failed.ID {
		t.Fatalf("rollback previous_version_id = %q, want %q", current.PreviousVersionID, failed.ID)
	}
}
