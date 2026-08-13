package types

import (
	"context"
	"strings"
	"testing"
)

func TestFileAcceptanceArtifactStoreReturnsChecksumAndRejectsTraversal(t *testing.T) {
	store := FileAcceptanceArtifactStore{Root: t.TempDir()}
	uri, checksum, size, err := store.Put(context.Background(), "run/report.json", strings.NewReader("report"))
	if err != nil || uri == "" || checksum == "" || size != 6 {
		t.Fatalf("unexpected artifact result: uri=%q checksum=%q size=%d err=%v", uri, checksum, size, err)
	}
	if _, _, _, err := store.Put(context.Background(), "../escape", strings.NewReader("x")); err == nil {
		t.Fatal("expected path traversal to be rejected")
	}
}

func TestAcceptanceMaterialChecklistReportsMissingEvidence(t *testing.T) {
	items := BuildAcceptanceMaterialChecklist([]AcceptanceArtifact{{Kind: AcceptanceMaterialReport, RunID: "run-1", URI: "report.json", SHA256: strings.Repeat("a", 64)}})
	if len(items) != 5 {
		t.Fatalf("expected five required material types, got %d", len(items))
	}
	for _, item := range items {
		if item.Kind == AcceptanceMaterialReport && !item.Present {
			t.Fatal("registered report should be present")
		}
		if item.Kind != AcceptanceMaterialReport && item.Present {
			t.Fatalf("unregistered material %q should be missing", item.Kind)
		}
	}
}
