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
			parse_status TEXT NOT NULL DEFAULT 'draft',
			current_version_id TEXT,
			pending_version_id TEXT,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE knowledge_version_reviews (
			id TEXT PRIMARY KEY,
			version_id TEXT NOT NULL,
			reviewer_id TEXT NOT NULL,
			action TEXT NOT NULL,
			comment TEXT,
			created_at DATETIME NOT NULL
		)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestUpdateKnowledgeIfPendingVersionRejectsStaleWorker(t *testing.T) {
	db := openKnowledgeGovernanceTestDB(t)
	repo := &knowledgeRepository{db: db}
	ctx := context.Background()
	if err := db.Exec(
		"INSERT INTO knowledges (id, tenant_id, parse_status, pending_version_id) VALUES (?, ?, ?, ?)",
		"knowledge-worker", 1, types.ParseStatusPending, "version-new",
	).Error; err != nil {
		t.Fatal(err)
	}

	updated, err := repo.UpdateKnowledgeIfPendingVersion(ctx, 1, "knowledge-worker", "version-old", map[string]any{
		"parse_status": types.ParseStatusProcessing,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated {
		t.Fatal("stale worker unexpectedly updated knowledge")
	}

	updated, err = repo.UpdateKnowledgeIfPendingVersion(ctx, 1, "knowledge-worker", "version-new", map[string]any{
		"parse_status": types.ParseStatusProcessing,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Fatal("current worker failed to update knowledge")
	}
	var status string
	if err := db.Table("knowledges").Where("id = ?", "knowledge-worker").Pluck("parse_status", &status).Error; err != nil {
		t.Fatal(err)
	}
	if status != types.ParseStatusProcessing {
		t.Fatalf("parse status = %q, want %q", status, types.ParseStatusProcessing)
	}
}

func TestGovernedKnowledgeVersionUpdatesDoNotCrossConcurrentVersions(t *testing.T) {
	db := openKnowledgeGovernanceTestDB(t)
	repo := &knowledgeRepository{db: db}
	ctx := context.Background()
	if err := db.Exec(
		"INSERT INTO knowledges (id, tenant_id, parse_status, current_version_id, pending_version_id) VALUES (?, ?, ?, ?, ?)",
		"knowledge-cas", 1, types.ParseStatusProcessing, "version-current", "version-pending",
	).Error; err != nil {
		t.Fatal(err)
	}

	updated, err := repo.SetPendingVersionIfCurrent(ctx, 1, "knowledge-cas", "version-old", "version-next")
	if err != nil {
		t.Fatal(err)
	}
	if updated {
		t.Fatal("stale pending-version setter unexpectedly updated knowledge")
	}
	updated, err = repo.SetPendingVersionIfCurrent(ctx, 1, "knowledge-cas", "version-pending", "version-next")
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Fatal("current pending-version setter failed to update knowledge")
	}

	if updated, err = repo.UpdateKnowledgeIfCurrentOrPendingVersion(ctx, 1, "knowledge-cas", "version-current", map[string]any{
		"parse_status": types.ParseStatusCompleted,
	}); err != nil {
		t.Fatal(err)
	} else if updated {
		t.Fatal("active old version updated while a newer pending version exists")
	}
}

func TestPrepareManagedUploadAutoApprovesAndMarksKnowledgePending(t *testing.T) {
	db := openKnowledgeGovernanceTestDB(t)
	repo := NewKnowledgeGovernanceRepository(db, nil)
	ctx := context.Background()
	version := newGovernedTestVersion(1, "knowledge-managed", string(types.KnowledgeVersionDraft), nil, nil)
	if err := repo.CreateVersion(ctx, version); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(
		"INSERT INTO knowledges (id, tenant_id, parse_status, pending_version_id) VALUES (?, ?, ?, ?)",
		"knowledge-managed", 1, types.ParseStatusDraft, version.ID,
	).Error; err != nil {
		t.Fatal(err)
	}

	if err := repo.PrepareManagedUpload(ctx, 1, "knowledge-managed", version.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetVersion(ctx, 1, version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != types.KnowledgeVersionIndexing {
		t.Fatalf("managed version status = %s, want indexing", stored.Status)
	}
	var knowledge struct {
		ParseStatus string `gorm:"column:parse_status"`
	}
	if err := db.Table("knowledges").Where("id = ?", "knowledge-managed").First(&knowledge).Error; err != nil {
		t.Fatal(err)
	}
	if knowledge.ParseStatus != types.ParseStatusPending {
		t.Fatalf("managed knowledge parse status = %s, want pending", knowledge.ParseStatus)
	}
	reviews, err := repo.ListReviews(ctx, version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 1 || reviews[0].Action != "auto_approve" || reviews[0].ReviewerID != "admin" {
		t.Fatalf("managed upload audit = %+v", reviews)
	}
}

func TestCreateVersionAndSetPendingIsAtomic(t *testing.T) {
	db := openKnowledgeGovernanceTestDB(t)
	repo := NewKnowledgeGovernanceRepository(db, nil)
	stager, ok := repo.(interface {
		CreateVersionAndSetPending(context.Context, *types.KnowledgeVersion, string, map[string]any) (bool, error)
	})
	if !ok {
		t.Fatal("governance repository does not support atomic version staging")
	}
	ctx := context.Background()
	if err := db.Exec("INSERT INTO knowledges (id, tenant_id, parse_status, pending_version_id) VALUES (?, ?, ?, ?)", "knowledge-stage", 1, types.ParseStatusDraft, "").Error; err != nil {
		t.Fatal(err)
	}

	version := newGovernedTestVersion(1, "knowledge-stage", string(types.KnowledgeVersionDraft), nil, nil)
	updated, err := stager.CreateVersionAndSetPending(ctx, version, "", map[string]any{"does_not_exist": "rollback"})
	if err == nil || updated {
		t.Fatalf("invalid atomic update = updated:%v err:%v, want rollback", updated, err)
	}
	if stored, getErr := repo.GetVersion(ctx, 1, version.ID); getErr != nil || stored != nil {
		t.Fatalf("version should roll back with knowledge update: %#v, err=%v", stored, getErr)
	}

	version = newGovernedTestVersion(1, "knowledge-stage", string(types.KnowledgeVersionDraft), nil, nil)
	updated, err = stager.CreateVersionAndSetPending(ctx, version, "", nil)
	if err != nil || !updated {
		t.Fatalf("atomic version staging failed: updated:%v err:%v", updated, err)
	}
	var pending string
	if err := db.Table("knowledges").Where("id = ?", "knowledge-stage").Pluck("pending_version_id", &pending).Error; err != nil {
		t.Fatal(err)
	}
	if pending != version.ID {
		t.Fatalf("pending version = %q, want %q", pending, version.ID)
	}
}

func TestTransitionVersionWithReviewRollsBackStatusWhenAuditFails(t *testing.T) {
	db := openKnowledgeGovernanceTestDB(t)
	repo := NewKnowledgeGovernanceRepository(db, nil)
	ctx := context.Background()
	version := newGovernedTestVersion(1, "knowledge-atomic", string(types.KnowledgeVersionRejected), nil, nil)
	if err := repo.CreateVersion(ctx, version); err != nil {
		t.Fatal(err)
	}
	reviewID := uuid.NewString()
	existing := &types.KnowledgeVersionReview{ID: reviewID, VersionID: version.ID, ReviewerID: "reviewer", Action: "existing", CreatedAt: time.Now().UTC()}
	if err := repo.CreateReview(ctx, existing); err != nil {
		t.Fatal(err)
	}

	err := repo.TransitionVersionWithReview(ctx, 1, version.ID, types.KnowledgeVersionPendingReview, &types.KnowledgeVersionReview{
		ID: reviewID, VersionID: version.ID, ReviewerID: "author", Action: "submit", CreatedAt: time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("duplicate audit ID should fail the transaction")
	}
	stored, getErr := repo.GetVersion(ctx, 1, version.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if stored.Status != types.KnowledgeVersionRejected {
		t.Fatalf("status = %s, want rejected after audit rollback", stored.Status)
	}
}

func TestTransitionVersionWithReviewUpdatesKnowledgeStatusAndSubmitIsIdempotent(t *testing.T) {
	db := openKnowledgeGovernanceTestDB(t)
	repo := NewKnowledgeGovernanceRepository(db, nil)
	ctx := context.Background()
	version := newGovernedTestVersion(1, "knowledge-submit", string(types.KnowledgeVersionDraft), nil, nil)
	if err := repo.CreateVersion(ctx, version); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(
		"INSERT INTO knowledges (id, tenant_id, parse_status, pending_version_id) VALUES (?, ?, ?, ?)",
		version.KnowledgeID, 1, types.ParseStatusDraft, version.ID,
	).Error; err != nil {
		t.Fatal(err)
	}

	submit := func() error {
		return repo.TransitionVersionWithReview(ctx, 1, version.ID, types.KnowledgeVersionPendingReview, &types.KnowledgeVersionReview{
			ID: uuid.NewString(), VersionID: version.ID, ReviewerID: "author", Action: "submit", CreatedAt: time.Now().UTC(),
		})
	}
	if err := submit(); err != nil {
		t.Fatal(err)
	}

	var knowledge struct {
		ParseStatus string `gorm:"column:parse_status"`
	}
	if err := db.Table("knowledges").Where("id = ?", version.KnowledgeID).First(&knowledge).Error; err != nil {
		t.Fatal(err)
	}
	if knowledge.ParseStatus != types.ParseStatusPendingReview {
		t.Fatalf("knowledge parse status = %s, want pending_review", knowledge.ParseStatus)
	}

	if err := db.Table("knowledges").Where("id = ?", version.KnowledgeID).Update("parse_status", types.ParseStatusDraft).Error; err != nil {
		t.Fatal(err)
	}
	if err := submit(); err != nil {
		t.Fatalf("repeated submit should be idempotent: %v", err)
	}
	if err := db.Table("knowledges").Where("id = ?", version.KnowledgeID).First(&knowledge).Error; err != nil {
		t.Fatal(err)
	}
	if knowledge.ParseStatus != types.ParseStatusPendingReview {
		t.Fatalf("repeated submit did not repair parse status: %s", knowledge.ParseStatus)
	}
	reviews, err := repo.ListReviews(ctx, version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 1 {
		t.Fatalf("repeated submit created %d reviews, want 1", len(reviews))
	}
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
	if err := db.Exec("INSERT INTO knowledges (id, tenant_id, current_version_id, pending_version_id) VALUES (?, ?, ?, ?)", "knowledge-1", 1, current.ID, candidate.ID).Error; err != nil {
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

func TestKnowledgeGovernanceActivationRejectsVersionReplacedByNewerPending(t *testing.T) {
	db := openKnowledgeGovernanceTestDB(t)
	repo := NewKnowledgeGovernanceRepository(db, nil)
	ctx := context.Background()
	candidate := newGovernedTestVersion(1, "knowledge-stale", string(types.KnowledgeVersionIndexing), nil, nil)
	if err := repo.CreateVersion(ctx, candidate); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(
		"INSERT INTO knowledges (id, tenant_id, pending_version_id) VALUES (?, ?, ?)",
		"knowledge-stale", 1, "version-newer",
	).Error; err != nil {
		t.Fatal(err)
	}

	if err := repo.ActivateVersion(ctx, 1, candidate.ID, time.Now().UTC()); err == nil {
		t.Fatal("stale candidate was activated")
	}
	stored, err := repo.GetVersion(ctx, 1, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != types.KnowledgeVersionIndexing {
		t.Fatalf("stale candidate status = %s, want indexing", stored.Status)
	}
}

func TestTransitionVersionWithReviewRejectsVersionReplacedByNewerPending(t *testing.T) {
	db := openKnowledgeGovernanceTestDB(t)
	repo := NewKnowledgeGovernanceRepository(db, nil)
	ctx := context.Background()
	candidate := newGovernedTestVersion(1, "knowledge-stale-review", string(types.KnowledgeVersionPendingReview), nil, nil)
	if err := repo.CreateVersion(ctx, candidate); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(
		"INSERT INTO knowledges (id, tenant_id, pending_version_id) VALUES (?, ?, ?)",
		"knowledge-stale-review", 1, "version-newer",
	).Error; err != nil {
		t.Fatal(err)
	}

	err := repo.TransitionVersionWithReview(ctx, 1, candidate.ID, types.KnowledgeVersionApproved, &types.KnowledgeVersionReview{
		ID: uuid.NewString(), VersionID: candidate.ID, ReviewerID: "reviewer", Action: "approve", CreatedAt: time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("stale review transition was accepted")
	}
	stored, err := repo.GetVersion(ctx, 1, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != types.KnowledgeVersionPendingReview {
		t.Fatalf("stale review version status = %s, want pending_review", stored.Status)
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
	if err := db.Exec("INSERT INTO knowledges (id, tenant_id, current_version_id, pending_version_id) VALUES (?, ?, ?, ?)", "knowledge-2", 1, current.ID, failed.ID).Error; err != nil {
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
