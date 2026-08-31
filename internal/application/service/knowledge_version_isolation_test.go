package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestGovernedVersionCanUpdateKnowledgeRejectsActiveVersionWithNewerPending(t *testing.T) {
	knowledge := &types.Knowledge{CurrentVersionID: "v1", PendingVersionID: "v2"}
	if governedVersionCanUpdateKnowledge(knowledge, "v1") {
		t.Fatal("active version was allowed to update while a newer version is pending")
	}
	if !governedVersionCanUpdateKnowledge(knowledge, "v2") {
		t.Fatal("pending version was not allowed to update")
	}

	knowledge.PendingVersionID = ""
	if !governedVersionCanUpdateKnowledge(knowledge, "v1") {
		t.Fatal("active version was not allowed to update after activation")
	}
}

func TestGovernedPassagesMetadataRoundTrip(t *testing.T) {
	want := []string{"第一段", "第二段"}
	got, err := decodeGovernedPassages(encodeGovernedPassages(want))
	if err != nil {
		t.Fatalf("decode governed passages: %v", err)
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("decoded passages = %#v, want %#v", got, want)
	}
}

func TestDecodeGovernedPassagesRejectsMissingContent(t *testing.T) {
	if _, err := decodeGovernedPassages(types.JSON([]byte(`{"other":"value"}`))); err == nil {
		t.Fatal("missing governed passage content was accepted")
	}
}
