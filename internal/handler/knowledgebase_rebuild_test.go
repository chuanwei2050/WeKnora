package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

type rebuildRecorder struct {
	calls  int
	failAt int
}

func (e *rebuildRecorder) reparse(_ context.Context, id string) (*types.Knowledge, error) {
	e.calls++
	if e.failAt > 0 && e.calls == e.failAt {
		return nil, errors.New("reparse failed")
	}
	return &types.Knowledge{ID: id}, nil
}

func TestReparseKnowledgeBaseItemsFiltersCompletedAndSupportsEmpty(t *testing.T) {
	e := &rebuildRecorder{}
	count, err := reparseKnowledgeBaseItems(context.Background(), nil, e.reparse)
	if err != nil || count != 0 || e.calls != 0 {
		t.Fatalf("empty rebuild = %d calls=%d err=%v", count, e.calls, err)
	}
	items := []*types.Knowledge{{ID: "ready", ParseStatus: types.ParseStatusCompleted}, {ID: "pending", ParseStatus: types.ParseStatusPending}}
	count, err = reparseKnowledgeBaseItems(context.Background(), items, e.reparse)
	if err != nil || count != 1 || e.calls != 1 {
		t.Fatalf("filtered rebuild = %d calls=%d err=%v", count, e.calls, err)
	}
}

func TestReparseKnowledgeBaseItemsStopsOnFailure(t *testing.T) {
	e := &rebuildRecorder{failAt: 2}
	items := []*types.Knowledge{{ID: "a", ParseStatus: types.ParseStatusCompleted}, {ID: "b", ParseStatus: types.ParseStatusCompleted}}
	count, err := reparseKnowledgeBaseItems(context.Background(), items, e.reparse)
	if err == nil || count != 1 || e.calls != 2 {
		t.Fatalf("failure rebuild = %d calls=%d err=%v", count, e.calls, err)
	}
}
