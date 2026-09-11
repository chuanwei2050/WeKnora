package types

import "testing"

func TestParseChunkMetadataPatch_Whitelist(t *testing.T) {
	p, err := ParseChunkMetadataPatch([]byte(`{"is_enabled":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if p.IsEnabled == nil || !*p.IsEnabled {
		t.Fatalf("expected is_enabled=true, got %+v", p)
	}

	_, err = ParseChunkMetadataPatch([]byte(`{"content":"x"}`))
	if err == nil {
		t.Fatal("expected reject for content")
	}
	_, err = ParseChunkMetadataPatch([]byte(`{"embedding":[1,2]}`))
	if err == nil {
		t.Fatal("expected reject for embedding")
	}
	_, err = ParseChunkMetadataPatch([]byte(`{}`))
	if err == nil {
		t.Fatal("expected reject for empty patch")
	}
}

func TestMergeChunkMetadataPatches(t *testing.T) {
	en := true
	m := MergeChunkMetadataPatches(map[string]bool{"c1": en}, map[string]string{"c1": "t1", "c2": "t2"})
	if m["c1"].IsEnabled == nil || !*m["c1"].IsEnabled || m["c1"].TagID == nil || *m["c1"].TagID != "t1" {
		t.Fatalf("c1 patch: %+v", m["c1"])
	}
	if m["c2"].TagID == nil || *m["c2"].TagID != "t2" || m["c2"].IsEnabled != nil {
		t.Fatalf("c2 patch: %+v", m["c2"])
	}
}

func TestIndexACLHotUpdateEnabledDefaultOff(t *testing.T) {
	t.Setenv("INDEX_ACL_HOT_UPDATE", "")
	if IndexACLHotUpdateEnabled() {
		t.Fatal("expected default off")
	}
	t.Setenv("INDEX_ACL_HOT_UPDATE", "true")
	if !IndexACLHotUpdateEnabled() {
		t.Fatal("expected on when true")
	}
}
