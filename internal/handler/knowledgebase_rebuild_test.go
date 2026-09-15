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
		t.Fatal("full rebuild must not target pending documents")
	}
	if !isDocumentPipelineTarget(pending, true) {
		t.Fatal("rechunk must target stuck pending documents")
	}
	if isDocumentPipelineTarget(&types.Knowledge{ID: "d", ParseStatus: types.ParseStatusDraft}, true) {
		t.Fatal("draft documents must stay excluded")
	}
}
