package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

type interruptByIDsRepo struct {
	markFailedRepo
	byID     map[string]*types.Knowledge
	updated  []*types.Knowledge
}

func (r *interruptByIDsRepo) GetKnowledgeBatch(_ context.Context, _ uint64, ids []string) ([]*types.Knowledge, error) {
	out := make([]*types.Knowledge, 0, len(ids))
	for _, id := range ids {
		item, ok := r.byID[id]
		if !ok || item == nil {
			continue
		}
		cp := *item
		out = append(out, &cp)
	}
	return out, nil
}

func (r *interruptByIDsRepo) GetKnowledgeByID(_ context.Context, _ uint64, id string) (*types.Knowledge, error) {
	item, ok := r.byID[id]
	if !ok || item == nil {
		return nil, nil
	}
	cp := *item
	return &cp, nil
}

func (r *interruptByIDsRepo) UpdateKnowledge(_ context.Context, knowledge *types.Knowledge) error {
	cp := *knowledge
	r.updated = append(r.updated, &cp)
	r.byID[knowledge.ID] = &cp
	return nil
}

func TestInterruptParsesByIDsOnlyTouchesPendingAndProcessing(t *testing.T) {
	repo := &interruptByIDsRepo{byID: map[string]*types.Knowledge{
		"pending":    {ID: "pending", TenantID: 1, ParseStatus: types.ParseStatusPending},
		"processing": {ID: "processing", TenantID: 1, ParseStatus: types.ParseStatusProcessing},
		"completed":  {ID: "completed", TenantID: 1, ParseStatus: types.ParseStatusCompleted},
		"failed":     {ID: "failed", TenantID: 1, ParseStatus: types.ParseStatusFailed, ErrorMessage: "old"},
		"other":      {ID: "other", TenantID: 1, ParseStatus: types.ParseStatusPending},
	}}
	svc := &knowledgeService{repo: repo}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))

	n, err := svc.InterruptParsesByIDs(ctx, []string{"pending", "processing", "completed", "failed"}, types.ParseInterruptedByUserCancelMessage)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("interrupted=%d, want 2", n)
	}
	if repo.byID["pending"].ParseStatus != types.ParseStatusFailed {
		t.Fatalf("pending status=%s", repo.byID["pending"].ParseStatus)
	}
	if repo.byID["pending"].ErrorMessage != types.ParseInterruptedByUserCancelMessage {
		t.Fatalf("pending message=%q", repo.byID["pending"].ErrorMessage)
	}
	if repo.byID["processing"].ParseStatus != types.ParseStatusFailed {
		t.Fatalf("processing status=%s", repo.byID["processing"].ParseStatus)
	}
	if repo.byID["completed"].ParseStatus != types.ParseStatusCompleted {
		t.Fatalf("completed was mutated")
	}
	if repo.byID["failed"].ErrorMessage != "old" {
		t.Fatalf("failed was mutated")
	}
	if repo.byID["other"].ParseStatus != types.ParseStatusPending {
		t.Fatalf("out-of-list doc was mutated")
	}
}

func TestInterruptParsesByIDsEmptyIsNoop(t *testing.T) {
	repo := &interruptByIDsRepo{byID: map[string]*types.Knowledge{}}
	svc := &knowledgeService{repo: repo}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	n, err := svc.InterruptParsesByIDs(ctx, nil, types.ParseInterruptedByUserCancelMessage)
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
