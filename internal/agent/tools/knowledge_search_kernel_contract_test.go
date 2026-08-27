package tools

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestAgentRetrievalAdapterPreservesAccessTenantAndRejectsBlankQuery(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	targets := types.SearchTargets{{KnowledgeBaseID: "kb", TenantID: 99}}
	normalized, err := normalizeAgentRetrievalRequest(ctx, []string{"query"}, targets, knowledgeSearchParams{})
	if err != nil {
		t.Fatal(err)
	}
	if normalized.TenantID != 7 {
		t.Fatalf("access tenant = %d, want requester tenant 7", normalized.TenantID)
	}
	if _, err := normalizeAgentRetrievalRequest(ctx, []string{" "}, targets, knowledgeSearchParams{}); err == nil {
		t.Fatal("agent adapter accepted a blank query")
	}
}
