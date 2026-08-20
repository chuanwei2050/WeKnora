package handler

import (
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
)

type rebuildTaskEnqueuer struct {
	calls  int
	failAt int
}

func (e *rebuildTaskEnqueuer) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	e.calls++
	if e.failAt > 0 && e.calls == e.failAt {
		return nil, errors.New("enqueue failed")
	}
	return &asynq.TaskInfo{ID: "task"}, nil
}

func TestEnqueueKnowledgePostProcessTasksFiltersCompletedAndSupportsEmpty(t *testing.T) {
	e := &rebuildTaskEnqueuer{}
	count, err := enqueueKnowledgePostProcessTasks(e, 1, "kb", nil)
	if err != nil || count != 0 || e.calls != 0 {
		t.Fatalf("empty rebuild = %d calls=%d err=%v", count, e.calls, err)
	}
	items := []*types.Knowledge{{ID: "ready", ParseStatus: types.ParseStatusCompleted}, {ID: "pending", ParseStatus: types.ParseStatusPending}}
	count, err = enqueueKnowledgePostProcessTasks(e, 1, "kb", items)
	if err != nil || count != 1 || e.calls != 1 {
		t.Fatalf("filtered rebuild = %d calls=%d err=%v", count, e.calls, err)
	}
}

func TestEnqueueKnowledgePostProcessTasksStopsOnFailure(t *testing.T) {
	e := &rebuildTaskEnqueuer{failAt: 2}
	items := []*types.Knowledge{{ID: "a", ParseStatus: types.ParseStatusCompleted}, {ID: "b", ParseStatus: types.ParseStatusCompleted}}
	count, err := enqueueKnowledgePostProcessTasks(e, 1, "kb", items)
	if err == nil || count != 1 || e.calls != 2 {
		t.Fatalf("failure rebuild = %d calls=%d err=%v", count, e.calls, err)
	}
}
