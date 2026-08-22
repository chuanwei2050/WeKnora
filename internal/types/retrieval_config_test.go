package types

import "testing"

func TestRetrievalConfigDefaults(t *testing.T) {
	var config *RetrievalConfig
	if got := config.GetEffectiveEmbeddingTopK(); got != 30 {
		t.Fatalf("expected default embedding top-k 30, got %d", got)
	}
	if got := config.GetEffectiveRerankTopK(); got != 5 {
		t.Fatalf("expected default rerank top-k 5, got %d", got)
	}
}
