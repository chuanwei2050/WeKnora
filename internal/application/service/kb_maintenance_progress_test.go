package service

import (
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestApplyKBMaintenanceCancelReparseReleasesLock(t *testing.T) {
	now := time.Date(2026, 9, 15, 6, 0, 0, 0, time.UTC)
	p := &types.KBMaintenanceProgress{
		Operation: types.KBMaintenanceReparse,
		Status:    "running",
		Total:     3,
	}
	applyKBMaintenanceCancel(p, now)
	if p.Status != "canceled" {
		t.Fatalf("reparse cancel should be canceled immediately, got %s", p.Status)
	}
	if p.FinishedAt == nil {
		t.Fatalf("canceled reparse must set FinishedAt")
	}
	if p.Message != "canceled_by_user" {
		t.Fatalf("unexpected message %q", p.Message)
	}
	if kbMaintenanceBusy(p.Status) {
		t.Fatalf("canceled must release the maintenance lock")
	}
}

func TestApplyKBMaintenanceCancelRechunkReleasesLock(t *testing.T) {
	now := time.Date(2026, 9, 15, 6, 0, 0, 0, time.UTC)
	p := &types.KBMaintenanceProgress{
		Operation: types.KBMaintenanceRechunk,
		Status:    "running",
		Total:     2,
	}
	applyKBMaintenanceCancel(p, now)
	if p.Status != "canceled" {
		t.Fatalf("rechunk cancel should be canceled immediately, got %s", p.Status)
	}
	if p.FinishedAt == nil {
		t.Fatalf("canceled rechunk must set FinishedAt")
	}
	if kbMaintenanceBusy(p.Status) {
		t.Fatalf("canceled must release the maintenance lock")
	}
}

func TestApplyKBMaintenanceCancelOtherOpsReleaseLock(t *testing.T) {
	now := time.Date(2026, 9, 15, 6, 0, 0, 0, time.UTC)
	p := &types.KBMaintenanceProgress{
		Operation: types.KBMaintenanceKeywords,
		Status:    "running",
	}
	applyKBMaintenanceCancel(p, now)
	if p.Status != "canceled" {
		t.Fatalf("non-reparse cancel should be canceled, got %s", p.Status)
	}
	if p.FinishedAt == nil {
		t.Fatalf("canceled job should set FinishedAt")
	}
	if kbMaintenanceBusy(p.Status) {
		t.Fatalf("canceled must release the maintenance lock")
	}
}

func TestApplyKBMaintenanceUpdateDoesNotOverwriteCanceled(t *testing.T) {
	now := time.Date(2026, 9, 15, 6, 0, 0, 0, time.UTC)
	p := &types.KBMaintenanceProgress{
		Status:    "canceled",
		Processed: 2,
		Failed:    0,
		Total:     5,
		Percent:   40,
	}
	if applyKBMaintenanceUpdate(p, 5, 0, "", true, now) {
		t.Fatalf("canceled progress must be immutable")
	}
	if p.Status != "canceled" || p.Processed != 2 || p.Percent != 40 {
		t.Fatalf("canceled progress was mutated: %+v", p)
	}
}

func TestApplyKBMaintenanceUpdateFinalizesCanceling(t *testing.T) {
	now := time.Date(2026, 9, 15, 6, 0, 0, 0, time.UTC)
	p := &types.KBMaintenanceProgress{
		Status: "canceling",
		Total:  4,
	}
	if !applyKBMaintenanceUpdate(p, 4, 1, "", true, now) {
		t.Fatalf("canceling finish should apply")
	}
	if p.Status != "canceled" {
		t.Fatalf("expected canceled, got %s", p.Status)
	}
	if p.FinishedAt == nil || p.Percent != 100 || p.Failed != 1 {
		t.Fatalf("unexpected finalized canceling state: %+v", p)
	}
}

func TestApplyKBMaintenanceUpdateKeepsCancelingUntilFinished(t *testing.T) {
	now := time.Date(2026, 9, 15, 6, 0, 0, 0, time.UTC)
	p := &types.KBMaintenanceProgress{
		Status: "canceling",
		Total:  4,
	}
	if !applyKBMaintenanceUpdate(p, 2, 0, "", false, now) {
		t.Fatalf("canceling progress updates should apply")
	}
	if p.Status != "canceling" || p.Processed != 2 || p.Percent != 50 {
		t.Fatalf("unexpected canceling progress: %+v", p)
	}
}
