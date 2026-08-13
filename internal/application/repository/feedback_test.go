package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openFeedbackTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE answer_feedback (
			id TEXT PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			session_id TEXT NOT NULL,
			message_id TEXT NOT NULL,
			answer_version TEXT NOT NULL,
			rating INTEGER NOT NULL,
			correction TEXT,
			target TEXT,
			status TEXT NOT NULL DEFAULT 'pending',
			reviewer_id TEXT,
			candidate_id TEXT,
			created_at DATETIME NOT NULL
		)`,
		`CREATE UNIQUE INDEX answer_feedback_message_version ON answer_feedback(message_id, answer_version)`,
		`CREATE TABLE feedback_candidates (
			id TEXT PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			feedback_id TEXT NOT NULL,
			target TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending_review',
			payload TEXT NOT NULL DEFAULT '{}',
			created_at DATETIME NOT NULL
		)`,
		`CREATE UNIQUE INDEX feedback_candidates_feedback ON feedback_candidates(tenant_id, feedback_id)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestAnswerFeedbackRepositoryCandidateCreationIsIdempotentAtDatabaseBoundary(t *testing.T) {
	db := openFeedbackTestDB(t)
	repo := NewAnswerFeedbackRepository(db)
	ctx := context.Background()
	feedback := &types.AnswerFeedback{
		ID: "feedback-1", TenantID: 7, SessionID: "session-1", MessageID: "message-1",
		AnswerVersion: "answer-v1", Rating: 1, Status: types.FeedbackPending,
	}
	if err := repo.Create(ctx, feedback); err != nil {
		t.Fatal(err)
	}

	first := &types.FeedbackCandidate{
		ID: "candidate-1", TenantID: 7, FeedbackID: feedback.ID,
		Target: types.FeedbackTargetKnowledgeDraft, Status: types.FeedbackCandidatePendingReview,
		Payload: map[string]interface{}{"answer_version": feedback.AnswerVersion},
	}
	if err := repo.CreateCandidate(ctx, first); err != nil {
		t.Fatal(err)
	}
	duplicate := *first
	duplicate.ID = "candidate-2"
	if err := repo.CreateCandidate(ctx, &duplicate); err == nil {
		t.Fatal("expected the tenant/feedback unique key to reject a duplicate candidate")
	}

	stored, err := repo.GetCandidateByFeedback(ctx, feedback.TenantID, feedback.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil || stored.ID != first.ID || stored.Status != types.FeedbackCandidatePendingReview {
		t.Fatalf("unexpected stored candidate: %+v", stored)
	}
}
