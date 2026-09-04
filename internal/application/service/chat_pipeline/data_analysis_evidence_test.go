package chatpipeline

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestDataAnalysisEvidenceInstructionPrioritizesMatchingFullTableResult(t *testing.T) {
	if strings.Contains(dataAnalysisEvidenceInstruction, "必须以 SQL") {
		t.Fatal("evidence fusion must not force the model to prefer SQL over retrieval evidence")
	}
	for _, required := range []string{"原始完整表格", "查询目标与用户问题一致", "作为确定结果", "ES、向量检索", "rerank", "交叉核对", "证据冲突"} {
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

func TestBuildDataAnalysisEvidenceIncludesNaturalLanguageScopeAndResult(t *testing.T) {
	got := buildDataAnalysisEvidence("硕士学历人员数量", `record 1: {"master_count":"41"}`)
	if !strings.Contains(got, "查询目标：硕士学历人员数量") || !strings.Contains(got, `"master_count":"41"`) {
		t.Fatalf("expected natural-language scope and structured result, got %q", got)
	}
	if strings.Contains(got, "SELECT") || strings.Contains(got, "Executed SQL") {
		t.Fatalf("evidence must not expose SQL, got %q", got)
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
