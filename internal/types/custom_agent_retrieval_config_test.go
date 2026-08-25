package types

import "testing"

func TestCustomAgentRetrievalDefaultsPreserveLegacyBehavior(t *testing.T) {
	agent := &CustomAgent{Config: CustomAgentConfig{
		RerankTopK: 5,
	}}

	agent.EnsureDefaults()

	if agent.Config.VectorRecallTopK != 50 || agent.Config.KeywordRecallTopK != 50 {
		t.Fatalf(
			"legacy recall defaults = vector:%d keyword:%d, want 50/50",
			agent.Config.VectorRecallTopK,
			agent.Config.KeywordRecallTopK,
		)
	}
	if agent.Config.RRFVectorWeight != 0.7 {
		t.Fatalf("RRF vector weight = %v, want 0.7", agent.Config.RRFVectorWeight)
	}
	if agent.Config.EmbeddingTopK != 30 || agent.Config.RerankCandidateTopK != 20 {
		t.Fatalf("fusion/rerank candidate counts = %d/%d, want 30/20", agent.Config.EmbeddingTopK, agent.Config.RerankCandidateTopK)
	}
	if err := agent.Config.Validate(); err != nil {
		t.Fatalf("legacy config with defaults should validate: %v", err)
	}
}

func TestCustomAgentRetrievalConfigRejectsInvalidStageOrder(t *testing.T) {
	config := CustomAgentConfig{
		EmbeddingTopK:       10,
		VectorRecallTopK:    50,
		KeywordRecallTopK:   50,
		RRFVectorWeight:     0.7,
		RerankCandidateTopK: 5,
		RerankTopK:          6,
	}

	if err := config.Validate(); err == nil {
		t.Fatal("expected rerank_top_k greater than rerank_candidate_top_k to fail")
	}
}

func TestCustomAgentMigratesPreviousRetrievalDefaults(t *testing.T) {
	agent := &CustomAgent{Config: CustomAgentConfig{
		EmbeddingTopK:       10,
		VectorRecallTopK:    50,
		KeywordRecallTopK:   50,
		RRFVectorWeight:     0.7,
		RerankCandidateTopK: 10,
		RerankTopK:          10,
	}}

	agent.EnsureDefaults()

	if agent.Config.EmbeddingTopK != 30 || agent.Config.RerankCandidateTopK != 20 || agent.Config.RerankTopK != 5 {
		t.Fatalf(
			"migrated retrieval stages = %d/%d/%d, want 30/20/5",
			agent.Config.EmbeddingTopK,
			agent.Config.RerankCandidateTopK,
			agent.Config.RerankTopK,
		)
	}
}

func TestCustomAgentMigratesPersistedLegacyZeroShape(t *testing.T) {
	agent := &CustomAgent{Config: CustomAgentConfig{
		EmbeddingTopK: 10,
		RerankTopK:    5,
	}}

	agent.EnsureDefaults()

	if agent.Config.EmbeddingTopK != 30 || agent.Config.RerankCandidateTopK != 20 || agent.Config.RerankTopK != 5 {
		t.Fatalf(
			"migrated zero-shape retrieval stages = %d/%d/%d, want 30/20/5",
			agent.Config.EmbeddingTopK,
			agent.Config.RerankCandidateTopK,
			agent.Config.RerankTopK,
		)
	}
}
