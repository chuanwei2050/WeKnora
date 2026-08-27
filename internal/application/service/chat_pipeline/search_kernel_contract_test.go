package chatpipeline

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestPipelineRetrievalAdapterPreservesAccessTenantAndRejectsBlankQuery(t *testing.T) {
	manage := &types.ChatManage{PipelineRequest: types.PipelineRequest{
		TenantID: 7, SearchTargets: types.SearchTargets{{KnowledgeBaseID: "kb"}},
	}}
	manage.RewriteQuery = "query"
	normalized, err := normalizePipelineRetrievalRequest(manage)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.TenantID != 7 {
		t.Fatalf("access tenant = %d, want 7", normalized.TenantID)
	}
	manage.RewriteQuery = " "
	if _, err := normalizePipelineRetrievalRequest(manage); err == nil {
		t.Fatal("pipeline adapter accepted a blank query")
	}
}
