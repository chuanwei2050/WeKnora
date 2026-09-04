package chatpipeline

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestDataAnalysisEvidenceInstructionBalancesSQLAndRetrieval(t *testing.T) {
	if strings.Contains(dataAnalysisEvidenceInstruction, "必须以 SQL") {
		t.Fatal("evidence fusion must not force the model to prefer SQL over retrieval evidence")
	}
	for _, required := range []string{"候选证据", "ES、向量检索", "rerank", "不要机械地优先采用任一来源", "查询条件", "数据覆盖范围", "证据冲突"} {
		if !strings.Contains(dataAnalysisEvidenceInstruction, required) {
			t.Fatalf("missing evidence fusion instruction %q", required)
		}
	}
	for _, forbiddenDetail := range []string{"SQL", "内部表名", "字段别名", "会话标识"} {
		if !strings.Contains(dataAnalysisEvidenceInstruction, forbiddenDetail) {
			t.Fatalf("missing user-facing disclosure constraint %q", forbiddenDetail)
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
