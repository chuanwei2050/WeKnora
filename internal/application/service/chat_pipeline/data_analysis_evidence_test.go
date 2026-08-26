package chatpipeline

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestDataAnalysisEvidenceInstructionBalancesSQLAndRetrieval(t *testing.T) {
	if strings.Contains(dataAnalysisEvidenceInstruction, "优先依据") {
		t.Fatal("ordinary fact queries must not force the model to prefer SQL over retrieval evidence")
	}
	for _, required := range []string{"交叉核对", "统计", "SQL 结果为准", "不得因 SQL 未命中而忽略其他证据"} {
		if !strings.Contains(dataAnalysisEvidenceInstruction, required) {
			t.Fatalf("missing evidence fusion instruction %q", required)
		}
	}
}

func TestDataAnalysisEvidenceIncludesAllRetrievedChunksForSelectedTable(t *testing.T) {
	results := []*types.SearchResult{
		{KnowledgeID: "selected", Content: "first sample"},
		{KnowledgeID: "other", Content: "unrelated sample"},
		{KnowledgeID: "selected", Content: "second sample"},
	}

	evidence := dataAnalysisEvidence(results, "selected", 1000)
	if !strings.Contains(evidence, "first sample") || !strings.Contains(evidence, "second sample") {
		t.Fatalf("expected all selected-table chunks, got %q", evidence)
	}
	if strings.Contains(evidence, "unrelated sample") {
		t.Fatalf("evidence must exclude other tables, got %q", evidence)
	}
}
