package chatpipeline

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestMergeDataAnalysisResultPreservesRerankedEvidenceOrder(t *testing.T) {
	documentResult := &types.SearchResult{ID: "document", Content: "department statistics"}
	analysisResult := &types.SearchResult{ID: "analysis", Content: "exact person"}

	got := mergeDataAnalysisResult(
		[]*types.SearchResult{documentResult},
		analysisResult,
		map[string]interface{}{"row_count": 1},
	)

	if len(got) != 2 || got[0] != documentResult || got[1] != analysisResult {
		t.Fatalf("expected reranked evidence followed by SQL evidence, got %#v", got)
	}
}

func TestMergeDataAnalysisResultRetainsRetrievalThatCanSupplementNarrowSQL(t *testing.T) {
	analysisResult := &types.SearchResult{ID: "analysis", Content: "SQL matched 夏雨欣"}
	retrievalResult := &types.SearchResult{ID: "retrieval", Content: "ES/vector also matched 许乃汉"}

	got := mergeDataAnalysisResult(
		[]*types.SearchResult{retrievalResult},
		analysisResult,
		map[string]interface{}{"row_count": 1},
	)

	if len(got) != 2 || got[0] != retrievalResult || got[1] != analysisResult {
		t.Fatalf("expected retrieval evidence followed by narrow SQL evidence, got %#v", got)
	}
}

func TestMergeDataAnalysisResultKeepsRetrievalFallbackForEmptySQLResult(t *testing.T) {
	documentResult := &types.SearchResult{ID: "document", Content: "retrieval fallback"}
	analysisResult := &types.SearchResult{ID: "analysis", Content: "no rows"}

	got := mergeDataAnalysisResult(
		[]*types.SearchResult{documentResult},
		analysisResult,
		map[string]interface{}{"row_count": 0},
	)

	if len(got) != 2 || got[0] != documentResult || got[1] != analysisResult {
		t.Fatalf("expected retrieval fallback followed by SQL result, got %#v", got)
	}
}

func TestMergeDataAnalysisResultDoesNotLetZeroCountHideRetrievalEvidence(t *testing.T) {
	retrievalResult := &types.SearchResult{ID: "retrieval", Content: "ES/vector matched multiple people"}
	analysisResult := &types.SearchResult{ID: "analysis", Content: `count_star(): "0"`}

	got := mergeDataAnalysisResult(
		[]*types.SearchResult{retrievalResult},
		analysisResult,
		map[string]interface{}{"row_count": 1},
	)

	if len(got) != 2 || got[0] != retrievalResult || got[1] != analysisResult {
		t.Fatalf("expected retrieval evidence before a zero aggregate, got %#v", got)
	}
}

func TestIsZeroAggregateResult(t *testing.T) {
	zero := &types.ToolResult{Data: map[string]interface{}{"rows": []map[string]string{{"total": "0"}}}}
	nonZero := &types.ToolResult{Data: map[string]interface{}{"rows": []map[string]string{{"total": "41"}}}}
	list := &types.ToolResult{Data: map[string]interface{}{"rows": []map[string]string{{"name": "Alice"}}}}
	if !isZeroAggregateResult(zero) || isZeroAggregateResult(nonZero) || isZeroAggregateResult(list) {
		t.Fatal("zero aggregate detection must only match a single numeric zero cell")
	}
}

func TestRecordDataAnalysisFailureForbidsDefiniteFragmentCount(t *testing.T) {
	chatManage := &types.ChatManage{PipelineState: types.PipelineState{MergeResult: []*types.SearchResult{{ID: "fragment", Content: "片段记录"}}}}
	target := &types.SearchResult{KnowledgeID: "knowledge", KnowledgeFilename: "people.xlsx"}

	recordDataAnalysisFailure(chatManage, target, "全表查询执行失败")

	if len(chatManage.MergeResult) != 2 || chatManage.MergeResult[0].ID != "analysis_failure_knowledge" {
		t.Fatalf("failure evidence was not prioritized: %#v", chatManage.MergeResult)
	}
	content := chatManage.MergeResult[0].Content
	if !strings.Contains(content, "不得根据检索片段") || !strings.Contains(content, "未能完成全表统计") {
		t.Fatalf("failure evidence does not constrain the final answer: %q", content)
	}
}
