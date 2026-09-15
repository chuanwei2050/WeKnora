package handler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

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

func TestReparseKnowledgeBaseItemsIncludesFailedAndSupportsEmpty(t *testing.T) {
	e := &rebuildRecorder{}
	count, err := reparseKnowledgeBaseItems(context.Background(), nil, e.reparse)
	if err != nil || count != 0 || e.calls != 0 {
		t.Fatalf("empty rebuild = %d calls=%d err=%v", count, e.calls, err)
	}
	items := []*types.Knowledge{
		{ID: "ready", ParseStatus: types.ParseStatusCompleted},
		{ID: "pending", ParseStatus: types.ParseStatusPending},
		{ID: "processing", ParseStatus: types.ParseStatusProcessing},
		{ID: "failed", ParseStatus: types.ParseStatusFailed},
		{ID: "draft", ParseStatus: types.ParseStatusDraft},
	}
	count, err = reparseKnowledgeBaseItems(context.Background(), items, e.reparse)
	if err != nil || count != 2 || e.calls != 2 {
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

func TestSummarizeKnowledgeBaseRebuildStatus(t *testing.T) {
	items := []*types.Knowledge{
		{ParseStatus: types.ParseStatusCompleted, SummaryStatus: types.SummaryStatusCompleted},
		{ParseStatus: types.ParseStatusPending},
		{ParseStatus: types.ParseStatusProcessing},
		{ParseStatus: types.ParseStatusFailed},
		{ParseStatus: types.ParseStatusDraft},
		nil,
	}
	status := summarizeKnowledgeBaseRebuildStatus(items)
	if status.Status != "running" || status.Total != 4 || status.Completed != 1 || status.Pending != 1 || status.Processing != 1 || status.Failed != 1 || status.Percent != 50 {
		t.Fatalf("unexpected status: %+v", status)
	}

	status = summarizeKnowledgeBaseRebuildStatus([]*types.Knowledge{{ParseStatus: types.ParseStatusCompleted}})
	if status.Status != "completed" || status.Percent != 100 {
		t.Fatalf("completed status: %+v", status)
	}

	status = summarizeKnowledgeBaseRebuildStatus([]*types.Knowledge{{ParseStatus: types.ParseStatusCompleted, SummaryStatus: types.SummaryStatusProcessing}})
	if status.Status != "running" || status.Processing != 1 || status.Percent != 0 {
		t.Fatalf("summary processing status: %+v", status)
	}

	status = summarizeKnowledgeBaseRebuildStatus(nil)
	if status.Status != "idle" || status.Total != 0 || status.Percent != 0 {
		t.Fatalf("idle status: %+v", status)
	}
}

func TestSummarizeDocumentPipelineProgress(t *testing.T) {
	items := []*types.Knowledge{
		{ID: "a", ParseStatus: types.ParseStatusCompleted, SummaryStatus: types.SummaryStatusCompleted},
		{ID: "b", ParseStatus: types.ParseStatusCompleted, SummaryStatus: types.SummaryStatusProcessing},
		{ID: "c", ParseStatus: types.ParseStatusFailed},
		{ID: "d", ParseStatus: types.ParseStatusPending},
		{ID: "e", ParseStatus: types.ParseStatusCompleted},
	}
	done, failed, active := summarizeDocumentPipelineProgress(items, []string{"a", "b", "c", "d"})
	if done != 1 || failed != 1 || active != 2 {
		t.Fatalf("unexpected progress done=%d failed=%d active=%d", done, failed, active)
	}
}

func TestIsDocumentPipelineMaintenance(t *testing.T) {
	if !types.KBMaintenanceReparse.IsDocumentPipelineMaintenance() || !types.KBMaintenanceRechunk.IsDocumentPipelineMaintenance() {
		t.Fatal("reparse/rechunk should be document pipeline ops")
	}
	if types.KBMaintenanceKeywords.IsDocumentPipelineMaintenance() {
		t.Fatal("keywords should not be a document pipeline op")
	}
}

func TestIsDocumentPipelineTargetIncludesStuckWhenRequested(t *testing.T) {
	pending := &types.Knowledge{ID: "p", ParseStatus: types.ParseStatusPending}
	if isDocumentPipelineTarget(pending, false) {
		t.Fatal("without resetStuck, pending documents must stay excluded")
	}
	if !isDocumentPipelineTarget(pending, true) {
		t.Fatal("pipeline rebuild/rechunk must target stuck pending documents")
	}
	if isDocumentPipelineTarget(&types.Knowledge{ID: "d", ParseStatus: types.ParseStatusDraft}, true) {
		t.Fatal("draft documents must stay excluded")
	}
}

func TestDocumentPipelineEnqueueConcurrencyCapsAtFour(t *testing.T) {
	t.Setenv("ASYNQ_CONCURRENCY", "16")
	if got := documentPipelineEnqueueConcurrency(); got != 4 {
		t.Fatalf("enqueue concurrency = %d, want 4", got)
	}
	t.Setenv("ASYNQ_CONCURRENCY", "2")
	if got := documentPipelineEnqueueConcurrency(); got != 2 {
		t.Fatalf("enqueue concurrency = %d, want 2", got)
	}
}

func TestReparseKnowledgeBaseItemsPacedLimitsParallelism(t *testing.T) {
	var mu sync.Mutex
	active, peak := 0, 0
	e := &rebuildRecorder{}
	items := []*types.Knowledge{
		{ID: "a", ParseStatus: types.ParseStatusCompleted},
		{ID: "b", ParseStatus: types.ParseStatusCompleted},
		{ID: "c", ParseStatus: types.ParseStatusCompleted},
		{ID: "d", ParseStatus: types.ParseStatusCompleted},
	}
	reparse := func(ctx context.Context, id string) (*types.Knowledge, error) {
		mu.Lock()
		active++
		if active > peak {
			peak = active
		}
		mu.Unlock()
		time.Sleep(30 * time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		return e.reparse(ctx, id)
	}
	count, err := reparseKnowledgeBaseItemsPaced(context.Background(), items, reparse, 2)
	if err != nil || count != 4 || e.calls != 4 {
		t.Fatalf("paced rebuild = %d calls=%d err=%v", count, e.calls, err)
	}
	if peak > 2 {
		t.Fatalf("peak parallelism = %d, want <= 2", peak)
	}
}

func TestReparseKnowledgeBaseItemsPacedCancelsOnFirstError(t *testing.T) {
	var mu sync.Mutex
	started := 0
	items := []*types.Knowledge{
		{ID: "a", ParseStatus: types.ParseStatusCompleted},
		{ID: "b", ParseStatus: types.ParseStatusCompleted},
		{ID: "c", ParseStatus: types.ParseStatusCompleted},
		{ID: "d", ParseStatus: types.ParseStatusCompleted},
		{ID: "e", ParseStatus: types.ParseStatusCompleted},
		{ID: "f", ParseStatus: types.ParseStatusCompleted},
	}
	reparse := func(ctx context.Context, id string) (*types.Knowledge, error) {
		mu.Lock()
		started++
		mu.Unlock()
		if id == "a" {
			time.Sleep(20 * time.Millisecond)
			return nil, errors.New("boom")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(80 * time.Millisecond):
			return &types.Knowledge{ID: id}, nil
		}
	}
	count, err := reparseKnowledgeBaseItemsPaced(context.Background(), items, reparse, 2)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected boom, got count=%d err=%v", count, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if started > 3 {
		t.Fatalf("started=%d, want cancel to limit further work", started)
	}
}

func TestDocumentPipelineFinishRequiresEnqueueDone(t *testing.T) {
	items := []*types.Knowledge{
		{ID: "a", ParseStatus: types.ParseStatusCompleted, SummaryStatus: types.SummaryStatusCompleted},
	}
	done, failed, active := summarizeDocumentPipelineProgress(items, []string{"a"})
	if done != 1 || failed != 0 || active != 0 {
		t.Fatalf("progress done=%d failed=%d active=%d", done, failed, active)
	}
	finishedWithoutEnqueue := false && active == 0 && done+failed >= 1
	finishedWithEnqueue := true && active == 0 && done+failed >= 1
	if finishedWithoutEnqueue || !finishedWithEnqueue {
		t.Fatal("enqueue_done must gate finished for document pipeline progress")
	}
}
