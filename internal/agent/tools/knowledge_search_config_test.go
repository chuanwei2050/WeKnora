package tools

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestNewKnowledgeSearchToolUsesConfiguredRerankValues(t *testing.T) {
	tool := NewKnowledgeSearchTool(nil, nil, nil, nil, nil, nil, nil, nil, 10)

	if tool.rerankTopK != 10 {
		t.Fatalf("rerankTopK = %d, want 10", tool.rerankTopK)
	}
	if got := tool.rerankResultLimit(18); got != 10 {
		t.Fatalf("rerankResultLimit(18) = %d, want 10", got)
	}
}

func TestNewKnowledgeSearchToolDefaultsRerankTopK(t *testing.T) {
	tool := NewKnowledgeSearchTool(nil, nil, nil, nil, nil, nil, nil, nil, 0)

	if tool.rerankTopK != 5 {
		t.Fatalf("rerankTopK = %d, want default 5", tool.rerankTopK)
	}
}

func TestKnowledgeSearchParamsPreferCurrentAgentConfig(t *testing.T) {
	agentConfig := &types.AgentConfig{
		EmbeddingTopK:       10,
		VectorRecallTopK:    50,
		KeywordRecallTopK:   50,
		RRFVectorWeight:     0.7,
		VectorThreshold:     0.42,
		KeywordThreshold:    0.24,
		RerankCandidateTopK: 10,
		RerankTopK:          5,
	}
	tool := NewKnowledgeSearchTool(nil, nil, nil, nil, nil, nil, nil, agentConfig, 5)
	tenant := &types.Tenant{ConversationConfig: &types.ConversationConfig{
		EmbeddingTopK:    3,
		VectorThreshold:  0.8,
		KeywordThreshold: 0.7,
	}}
	ctx := context.WithValue(t.Context(), types.TenantInfoContextKey, tenant)

	params := tool.resolveSearchParams(ctx)

	if params.topK != 10 || params.vectorRecallTopK != 50 || params.keywordRecallTopK != 50 {
		t.Fatalf("retrieval counts = fusion:%d vector:%d keyword:%d, want 10/50/50", params.topK, params.vectorRecallTopK, params.keywordRecallTopK)
	}
	if params.rrfVectorWeight != 0.7 {
		t.Fatalf("RRF vector weight = %v, want 0.7", params.rrfVectorWeight)
	}
	if params.vectorThreshold != 0.42 || params.keywordThreshold != 0.24 {
		t.Fatalf("thresholds = vector:%v keyword:%v, want 0.42/0.24", params.vectorThreshold, params.keywordThreshold)
	}
}

func TestKnowledgeSearchParamsUseEffectiveDefaultsWithoutAgentConfig(t *testing.T) {
	tool := NewKnowledgeSearchTool(nil, nil, nil, nil, nil, nil, nil, nil, 5)

	params := tool.resolveSearchParams(t.Context())

	if params.topK != 30 || params.vectorRecallTopK != 50 || params.keywordRecallTopK != 50 {
		t.Fatalf("default retrieval counts = fusion:%d vector:%d keyword:%d, want 30/50/50", params.topK, params.vectorRecallTopK, params.keywordRecallTopK)
	}
	if params.rrfVectorWeight != 0.7 {
		t.Fatalf("default RRF vector weight = %v, want 0.7", params.rrfVectorWeight)
	}
}

func TestKnowledgeSearchParamsPreserveExplicitZeroThresholds(t *testing.T) {
	agentConfig := &types.AgentConfig{
		EmbeddingTopK:       30,
		VectorRecallTopK:    50,
		KeywordRecallTopK:   50,
		RRFVectorWeight:     0.7,
		RerankCandidateTopK: 20,
		RerankTopK:          5,
	}
	tool := NewKnowledgeSearchTool(nil, nil, nil, nil, nil, nil, nil, agentConfig, 5)
	tenant := &types.Tenant{ConversationConfig: &types.ConversationConfig{
		VectorThreshold:  0.8,
		KeywordThreshold: 0.7,
	}}
	ctx := context.WithValue(t.Context(), types.TenantInfoContextKey, tenant)

	params := tool.resolveSearchParams(ctx)

	if params.vectorThreshold != 0 || params.keywordThreshold != 0 {
		t.Fatalf("explicit zero thresholds = vector:%v keyword:%v, want 0/0", params.vectorThreshold, params.keywordThreshold)
	}
}

func TestDeduplicateResultsSortsByScoreBeforeCandidateLimit(t *testing.T) {
	tool := NewKnowledgeSearchTool(nil, nil, nil, nil, nil, nil, nil, nil, 5)
	results := []*searchResultWithMeta{
		{SearchResult: &types.SearchResult{ID: "low", Score: 0.1}},
		{SearchResult: &types.SearchResult{ID: "high", Score: 0.9}},
		{SearchResult: &types.SearchResult{ID: "middle", Score: 0.5}},
	}

	deduplicated := tool.deduplicateResults(results)

	if len(deduplicated) != 3 || deduplicated[0].ID != "high" || deduplicated[1].ID != "middle" || deduplicated[2].ID != "low" {
		t.Fatalf("deduplicated order = %v/%v/%v, want high/middle/low", deduplicated[0].ID, deduplicated[1].ID, deduplicated[2].ID)
	}
}
