package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestApplyGraphRebuildIncr(t *testing.T) {
	p := &types.GraphRebuildProgress{
		Status: types.GraphRebuildRunning,
		Total:  2,
	}
	applyGraphRebuildIncr(p)
	if p.Processed != 1 || p.Status != types.GraphRebuildRunning {
		t.Fatalf("first incr should stay running, got processed=%d status=%s", p.Processed, p.Status)
	}
	applyGraphRebuildIncr(p)
	if p.Processed != 2 || p.Status != types.GraphRebuildCompleted || p.Percent != 100 {
		t.Fatalf("second incr should complete, got processed=%d status=%s percent=%d", p.Processed, p.Status, p.Percent)
	}

	review := &types.GraphRebuildProgress{
		Status:        types.GraphRebuildRunning,
		Total:         1,
		RequireReview: true,
	}
	applyGraphRebuildIncr(review)
	if review.Status != types.GraphRebuildAwaitingReview {
		t.Fatalf("require_review should await review, got %s", review.Status)
	}
}

func TestCalcPercent(t *testing.T) {
	if got := calcPercent(1, 4, types.GraphRebuildRunning); got != 25 {
		t.Fatalf("expected 25, got %d", got)
	}
	if got := calcPercent(0, 0, types.GraphRebuildRunning); got != 5 {
		t.Fatalf("running with unknown total should show small progress, got %d", got)
	}
	if got := calcPercent(0, 0, types.GraphRebuildCompleted); got != 100 {
		t.Fatalf("completed should be 100, got %d", got)
	}
}
