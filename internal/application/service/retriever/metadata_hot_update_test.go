package retriever

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

type recordingMetadataUpdater struct {
	enabled map[string]bool
	tags    map[string]string
}

func (r *recordingMetadataUpdater) BatchUpdateChunkEnabledStatus(_ context.Context, m map[string]bool) error {
	r.enabled = m
	return nil
}
func (r *recordingMetadataUpdater) BatchUpdateChunkTagID(_ context.Context, m map[string]string) error {
	r.tags = m
	return nil
}

func TestApplyChunkMetadataPatches(t *testing.T) {
	updater := &recordingMetadataUpdater{}
	enabled := true
	tag := "folder-1"
	err := ApplyChunkMetadataPatches(context.Background(), updater, map[string]types.ChunkMetadataPatch{
		"c1": {IsEnabled: &enabled},
		"c2": {TagID: &tag},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updater.enabled["c1"] || updater.tags["c2"] != "folder-1" {
		t.Fatalf("unexpected patches: enabled=%v tags=%v", updater.enabled, updater.tags)
	}
	err = ApplyChunkMetadataPatches(context.Background(), updater, map[string]types.ChunkMetadataPatch{
		"c3": {},
	})
	if err == nil {
		t.Fatal("expected validation error for empty patch")
	}
}

func TestApplyChunkMetadataPatches_Combined(t *testing.T) {
	updater := &recordingMetadataUpdater{}
	enabled := false
	tag := "t9"
	err := ApplyChunkMetadataPatches(context.Background(), updater, map[string]types.ChunkMetadataPatch{
		"c1": {IsEnabled: &enabled, TagID: &tag},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updater.enabled["c1"] != false || updater.tags["c1"] != "t9" {
		t.Fatalf("combined patch failed: enabled=%v tags=%v", updater.enabled, updater.tags)
	}
}

func TestApplyChunkMetadataPatches_EmptyMap(t *testing.T) {
	updater := &recordingMetadataUpdater{}
	if err := ApplyChunkMetadataPatches(context.Background(), updater, nil); err != nil {
		t.Fatal(err)
	}
	if updater.enabled != nil || updater.tags != nil {
		t.Fatal("empty updates should not call engine")
	}
}
