package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

type markFailedRepo struct {
	knowledge *types.Knowledge
	updated   *types.Knowledge
}

func (r *markFailedRepo) CreateKnowledge(context.Context, *types.Knowledge) error { return nil }
func (r *markFailedRepo) GetKnowledgeByID(_ context.Context, _ uint64, id string) (*types.Knowledge, error) {
	if r.knowledge == nil || r.knowledge.ID != id {
		return nil, nil
	}
	cp := *r.knowledge
	return &cp, nil
}
func (r *markFailedRepo) GetKnowledgeByIDOnly(context.Context, string) (*types.Knowledge, error) {
	return nil, nil
}
func (r *markFailedRepo) ListKnowledgeByKnowledgeBaseID(context.Context, uint64, string) ([]*types.Knowledge, error) {
	return nil, nil
}
func (r *markFailedRepo) ListPagedKnowledgeByKnowledgeBaseID(context.Context, uint64, string, *types.Pagination, string, string, string) ([]*types.Knowledge, int64, error) {
	return nil, 0, nil
}
func (r *markFailedRepo) ListPagedKnowledgeByDirectory(context.Context, uint64, string, *string, int, int, string, string, string, string, string, types.KnowledgeVisibilityFilter) ([]*types.Knowledge, int64, error) {
	return nil, 0, nil
}
func (r *markFailedRepo) UpdateKnowledge(_ context.Context, knowledge *types.Knowledge) error {
	cp := *knowledge
	r.updated = &cp
	return nil
}
func (r *markFailedRepo) UpdateKnowledgeBatch(context.Context, []*types.Knowledge) error { return nil }
func (r *markFailedRepo) ClaimDirectoryDeletionStorage(context.Context, uint64, string, []string) (int64, error) {
	return 0, nil
}
func (r *markFailedRepo) DeleteKnowledge(context.Context, uint64, string) error { return nil }
func (r *markFailedRepo) DeleteKnowledgeList(context.Context, uint64, []string) error {
	return nil
}
func (r *markFailedRepo) GetKnowledgeBatch(context.Context, uint64, []string) ([]*types.Knowledge, error) {
	return nil, nil
}
func (r *markFailedRepo) CheckKnowledgeExists(context.Context, uint64, string, *types.KnowledgeCheckParams) (bool, *types.Knowledge, error) {
	return false, nil, nil
}
func (r *markFailedRepo) AminusB(context.Context, uint64, string, uint64, string) ([]string, error) {
	return nil, nil
}
func (r *markFailedRepo) UpdateKnowledgeColumn(context.Context, string, string, interface{}) error {
	return nil
}
func (r *markFailedRepo) FailStaleProcessingKnowledge(context.Context, time.Time, string) (int64, error) {
	return 0, nil
}
func (r *markFailedRepo) CountKnowledgeByKnowledgeBaseID(context.Context, uint64, string) (int64, error) {
	return 0, nil
}
func (r *markFailedRepo) CountKnowledgeByStatus(context.Context, uint64, string, []string) (int64, error) {
	return 0, nil
}
func (r *markFailedRepo) SearchKnowledge(context.Context, uint64, string, int, int, []string) ([]*types.Knowledge, bool, error) {
	return nil, false, nil
}
func (r *markFailedRepo) FindByMetadataKey(context.Context, uint64, string, string, string) (*types.Knowledge, error) {
	return nil, nil
}
func (r *markFailedRepo) SearchKnowledgeInScopes(context.Context, []types.KnowledgeSearchScope, string, int, int, []string) ([]*types.Knowledge, bool, error) {
	return nil, false, nil
}
func (r *markFailedRepo) ListIDsByTagID(context.Context, uint64, string, string) ([]string, error) {
	return nil, nil
}

func TestMarkDocumentProcessFailedUpdatesProcessing(t *testing.T) {
	repo := &markFailedRepo{knowledge: &types.Knowledge{
		ID:          "k1",
		TenantID:    1,
		ParseStatus: types.ParseStatusProcessing,
	}}
	svc := &knowledgeService{repo: repo}
	payload, _ := json.Marshal(types.DocumentProcessPayload{
		TenantID:    1,
		KnowledgeID: "k1",
	})

	if err := svc.MarkDocumentProcessFailed(context.Background(), payload, errors.New("lease expired")); err != nil {
		t.Fatal(err)
	}
	if repo.updated == nil {
		t.Fatal("expected knowledge update")
	}
	if repo.updated.ParseStatus != types.ParseStatusFailed {
		t.Fatalf("parse_status=%s", repo.updated.ParseStatus)
	}
	if repo.updated.ErrorMessage != "lease expired" {
		t.Fatalf("error_message=%s", repo.updated.ErrorMessage)
	}
}

func TestMarkDocumentProcessFailedSkipsCompleted(t *testing.T) {
	repo := &markFailedRepo{knowledge: &types.Knowledge{
		ID:          "k1",
		TenantID:    1,
		ParseStatus: types.ParseStatusCompleted,
	}}
	svc := &knowledgeService{repo: repo}
	payload, _ := json.Marshal(types.DocumentProcessPayload{
		TenantID:    1,
		KnowledgeID: "k1",
	})

	if err := svc.MarkDocumentProcessFailed(context.Background(), payload, errors.New("lease expired")); err != nil {
		t.Fatal(err)
	}
	if repo.updated != nil {
		t.Fatal("completed knowledge must not be overwritten")
	}
}
