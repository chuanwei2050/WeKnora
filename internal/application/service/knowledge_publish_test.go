package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
)

type publishActivateRepo struct {
	interfaces.KnowledgeGovernanceRepository
	version   *types.KnowledgeVersion
	activated string
}

func (s *publishActivateRepo) GetVersion(_ context.Context, _ uint64, id string) (*types.KnowledgeVersion, error) {
	if s.version == nil || s.version.ID != id {
		return nil, nil
	}
	cp := *s.version
	return &cp, nil
}

func (s *publishActivateRepo) ActivateVersion(_ context.Context, _ uint64, id string, _ time.Time) error {
	s.activated = id
	if s.version != nil {
		s.version.Status = types.KnowledgeVersionActive
	}
	return nil
}

func (s *publishActivateRepo) UpdateVersionStatus(_ context.Context, _ uint64, _ string, _ types.KnowledgeVersionStatus) error {
	return nil
}

func (s *publishActivateRepo) ListReviews(_ context.Context, _ string) ([]*types.KnowledgeVersionReview, error) {
	return []*types.KnowledgeVersionReview{{Action: "publish"}}, nil
}

func (s *publishActivateRepo) CreateReview(_ context.Context, _ *types.KnowledgeVersionReview) error {
	return nil
}

type publishKnowledgeRepoStub struct {
	interfaces.KnowledgeRepository
	knowledge *types.Knowledge
}

func (s *publishKnowledgeRepoStub) GetKnowledgeByID(_ context.Context, _ uint64, _ string) (*types.Knowledge, error) {
	if s.knowledge == nil {
		return nil, nil
	}
	cp := *s.knowledge
	return &cp, nil
}

type publishPurgerStub struct {
	calls []string
}

func (s *publishPurgerStub) PurgeAfterActivation(_ context.Context, _ uint64, knowledgeID, supersededVersionID string) error {
	s.calls = append(s.calls, knowledgeID+"|"+supersededVersionID)
	return nil
}

func (s *publishPurgerStub) ReconcileCurrentVersionIndexes(_ context.Context, _ uint64, knowledgeID string) error {
	s.calls = append(s.calls, knowledgeID+"|reconcile")
	return nil
}

func TestKnowledgePublishHandlePurgesPreviousCurrentVersionIndexes(t *testing.T) {
	version := &types.KnowledgeVersion{
		ID:          "version-new",
		KnowledgeID: "doc-1",
		TenantID:    7,
		Status:      types.KnowledgeVersionIndexing,
	}
	repo := &publishActivateRepo{version: version}
	knowledgeRepo := &publishKnowledgeRepoStub{knowledge: &types.Knowledge{
		ID: "doc-1", CurrentVersionID: "version-old", PendingVersionID: "version-new",
	}}
	purger := &publishPurgerStub{}
	svc := &KnowledgePublishService{
		repo:          repo,
		knowledgeRepo: knowledgeRepo,
		indexPurger:   purger,
	}
	payload, _ := json.Marshal(types.KnowledgePublishPayload{
		TenantID: 7, KnowledgeID: "doc-1", VersionID: "version-new",
	})
	if err := svc.Handle(context.Background(), asynq.NewTask(types.TypeKnowledgePublish, payload)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if repo.activated != "version-new" {
		t.Fatalf("activated = %q", repo.activated)
	}
	if len(purger.calls) != 1 || purger.calls[0] != "doc-1|reconcile" {
		t.Fatalf("purge calls = %v", purger.calls)
	}
}

func TestKnowledgePublishHandleReconcilesWhenAlreadyActive(t *testing.T) {
	version := &types.KnowledgeVersion{
		ID:          "version-new",
		KnowledgeID: "doc-1",
		TenantID:    7,
		Status:      types.KnowledgeVersionActive,
	}
	repo := &publishActivateRepo{version: version}
	purger := &publishPurgerStub{}
	svc := &KnowledgePublishService{
		repo:        repo,
		indexPurger: purger,
	}
	payload, _ := json.Marshal(types.KnowledgePublishPayload{
		TenantID: 7, KnowledgeID: "doc-1", VersionID: "version-new",
	})
	if err := svc.Handle(context.Background(), asynq.NewTask(types.TypeKnowledgePublish, payload)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if repo.activated != "" {
		t.Fatalf("should not re-activate, got %q", repo.activated)
	}
	if len(purger.calls) != 1 || purger.calls[0] != "doc-1|reconcile" {
		t.Fatalf("expected reconcile on active retry, got %v", purger.calls)
	}
}
