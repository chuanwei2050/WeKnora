package chatpipeline

import "testing"

func TestUnwrapAgentDraftText(t *testing.T) {
	wrapped := `{"draft":{"id":"pipeline-draft","text":"Alpha依赖Beta"},"evidence":{"items":[]}}`
	if got := unwrapAgentDraftText(wrapped); got != "Alpha依赖Beta" {
		t.Fatalf("unwrapAgentDraftText() = %q, want plain draft text", got)
	}
}

func TestUnwrapAgentDraftTextPreservesOrdinaryJSON(t *testing.T) {
	plain := `{"answer":"Alpha依赖Beta"}`
	if got := unwrapAgentDraftText(plain); got != plain {
		t.Fatalf("unwrapAgentDraftText() changed ordinary JSON: %q", got)
	}
}
