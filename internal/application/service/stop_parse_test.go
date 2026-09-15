package service

import (
	"context"
	"testing"

	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

func TestStopParseKnowledgeInterruptsAndMarksFailed(t *testing.T) {
	repo := &interruptByIDsRepo{byID: map[string]*types.Knowledge{
		"doc1": {ID: "doc1", TenantID: 1, ParseStatus: types.ParseStatusProcessing},
	}}
	svc := &knowledgeService{repo: repo}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))

	got, err := svc.StopParseKnowledge(ctx, "doc1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ParseStatus != types.ParseStatusFailed {
		t.Fatalf("status=%s", got.ParseStatus)
	}
	if got.ErrorMessage != types.ParseInterruptedByUserStopMessage {
		t.Fatalf("message=%q", got.ErrorMessage)
	}
}

func TestStopParseKnowledgeRejectsNonParsing(t *testing.T) {
	repo := &interruptByIDsRepo{byID: map[string]*types.Knowledge{
		"done": {ID: "done", TenantID: 1, ParseStatus: types.ParseStatusCompleted},
	}}
	svc := &knowledgeService{repo: repo}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))

	_, err := svc.StopParseKnowledge(ctx, "done")
	if err == nil {
		t.Fatal("expected error")
	}
	if appErr, ok := werrors.IsAppError(err); !ok || appErr.Code != werrors.ErrBadRequest {
		t.Fatalf("want bad request, got %v", err)
	}
	if repo.byID["done"].ParseStatus != types.ParseStatusCompleted {
		t.Fatal("completed doc was mutated")
	}
}

func TestStopParseKnowledgeBatchOnlyInterruptsParsing(t *testing.T) {
	repo := &interruptByIDsRepo{byID: map[string]*types.Knowledge{
		"pending":    {ID: "pending", TenantID: 1, ParseStatus: types.ParseStatusPending},
		"processing": {ID: "processing", TenantID: 1, ParseStatus: types.ParseStatusProcessing},
		"completed":  {ID: "completed", TenantID: 1, ParseStatus: types.ParseStatusCompleted},
	}}
	svc := &knowledgeService{repo: repo}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))

	n, ids, err := svc.StopParseKnowledgeBatch(ctx, []string{"pending", "processing", "completed", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("interrupted=%d want 2", n)
	}
	if len(ids) != 2 {
		t.Fatalf("ids=%v", ids)
	}
	if repo.byID["completed"].ParseStatus != types.ParseStatusCompleted {
		t.Fatal("completed was mutated")
	}
	if repo.byID["pending"].ErrorMessage != types.ParseInterruptedByUserStopMessage {
		t.Fatalf("pending message=%q", repo.byID["pending"].ErrorMessage)
	}
}

func TestStopParseKnowledgeBatchRejectsWhenNoneParsing(t *testing.T) {
	repo := &interruptByIDsRepo{byID: map[string]*types.Knowledge{
		"done": {ID: "done", TenantID: 1, ParseStatus: types.ParseStatusCompleted},
	}}
	svc := &knowledgeService{repo: repo}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))

	_, _, err := svc.StopParseKnowledgeBatch(ctx, []string{"done"})
	if err == nil {
		t.Fatal("expected error")
	}
	if appErr, ok := werrors.IsAppError(err); !ok || appErr.Code != werrors.ErrBadRequest {
		t.Fatalf("want bad request, got %v", err)
	}
}

func (r *interruptByIDsRepo) ListKnowledgeByKnowledgeBaseID(_ context.Context, _ uint64, kbID string) ([]*types.Knowledge, error) {
	out := make([]*types.Knowledge, 0, len(r.byID))
	for _, item := range r.byID {
		if item == nil || item.KnowledgeBaseID != kbID {
			continue
		}
		cp := *item
		out = append(out, &cp)
	}
	return out, nil
}

func TestStopAllParsesInKnowledgeBase(t *testing.T) {
	repo := &interruptByIDsRepo{byID: map[string]*types.Knowledge{
		"a": {ID: "a", TenantID: 1, KnowledgeBaseID: "kb1", ParseStatus: types.ParseStatusPending},
		"b": {ID: "b", TenantID: 1, KnowledgeBaseID: "kb1", ParseStatus: types.ParseStatusProcessing},
		"c": {ID: "c", TenantID: 1, KnowledgeBaseID: "kb1", ParseStatus: types.ParseStatusCompleted},
		"d": {ID: "d", TenantID: 1, KnowledgeBaseID: "kb2", ParseStatus: types.ParseStatusPending},
	}}
	svc := &knowledgeService{repo: repo}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))

	n, ids, err := svc.StopAllParsesInKnowledgeBase(ctx, "kb1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("interrupted=%d want 2 ids=%v", n, ids)
	}
	if repo.byID["c"].ParseStatus != types.ParseStatusCompleted {
		t.Fatal("completed in kb1 was mutated")
	}
	if repo.byID["d"].ParseStatus != types.ParseStatusPending {
		t.Fatal("other kb doc was mutated")
	}
}
