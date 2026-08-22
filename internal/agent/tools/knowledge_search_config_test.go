package tools

import "testing"

func TestNewKnowledgeSearchToolUsesConfiguredRerankValues(t *testing.T) {
	tool := NewKnowledgeSearchTool(nil, nil, nil, nil, nil, nil, nil, 10)

	if tool.rerankTopK != 10 {
		t.Fatalf("rerankTopK = %d, want 10", tool.rerankTopK)
	}
	if got := tool.rerankResultLimit(18); got != 10 {
		t.Fatalf("rerankResultLimit(18) = %d, want 10", got)
	}
}

func TestNewKnowledgeSearchToolDefaultsRerankTopK(t *testing.T) {
	tool := NewKnowledgeSearchTool(nil, nil, nil, nil, nil, nil, nil, 0)

	if tool.rerankTopK != 5 {
		t.Fatalf("rerankTopK = %d, want default 5", tool.rerankTopK)
	}
}
