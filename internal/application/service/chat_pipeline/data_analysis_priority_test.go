package chatpipeline

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/types"
)

func TestDataAnalysisInitialAction(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    tools.DataAnalysisAction
	}{
		{name: "skip", content: `{"action":"skip","knowledge_id":"model-value"}`, want: tools.DataAnalysisActionSkip},
		{name: "execute", content: "```json\n{\"action\":\"execute\",\"sql\":\"SELECT 1\"}\n```", want: tools.DataAnalysisActionExecute},
		{name: "invalid", content: `not-json`, want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := dataAnalysisInitialAction(test.content, "expected-knowledge"); got != test.want {
				t.Fatalf("dataAnalysisInitialAction() = %q, want %q", got, test.want)
			}
		})
	}
}

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

func TestRecordDataAnalysisFailureForbidsDefiniteFragmentCount(t *testing.T) {
	chatManage := &types.ChatManage{PipelineState: types.PipelineState{MergeResult: []*types.SearchResult{{ID: "fragment", Content: "片段记录"}}}}
	target := &types.SearchResult{KnowledgeID: "knowledge", KnowledgeFilename: "people.xlsx"}

	recordDataAnalysisFailure(chatManage, target, "全表查询执行失败")

	if len(chatManage.MergeResult) != 2 || chatManage.MergeResult[0].ID != "analysis_failure_knowledge" {
		t.Fatalf("failure evidence was not prioritized: %#v", chatManage.MergeResult)
	}
	content := chatManage.MergeResult[0].Content
	if !strings.Contains(content, "不得根据检索片段") || !strings.Contains(content, "表格查询未完成") {
		t.Fatalf("failure evidence does not constrain the final answer: %q", content)
	}
}

func TestSelectUniqueDataAnalysisCandidatesUsesFileHashAndFillsLimit(t *testing.T) {
	input := []dataAnalysisCandidate{
		{knowledge: &types.Knowledge{ID: "first", FileName: "people.xlsx", FileHash: "same-content"}},
		{knowledge: &types.Knowledge{ID: "copy", FileName: "renamed.xlsx", FileHash: "same-content"}},
		{knowledge: &types.Knowledge{ID: "other", FileName: "people.xlsx", FileHash: "different-content"}},
		{knowledge: &types.Knowledge{ID: "legacy", FileName: "people.xlsx"}},
	}
	got := make([]dataAnalysisCandidate, 0, 3)
	hashIndexes := make(map[string]int, 3)
	duplicates := 0
	for _, candidate := range input {
		var duplicate bool
		got, duplicate = appendDataAnalysisCandidate(got, hashIndexes, candidate)
		if duplicate {
			duplicates++
		}
		if len(got) == 3 {
			break
		}
	}
	if duplicates != 1 || len(got) != 3 {
		t.Fatalf("duplicates=%d candidates=%#v", duplicates, got)
	}
	if got[0].knowledge.ID != "first" || got[1].knowledge.ID != "other" || got[2].knowledge.ID != "legacy" {
		t.Fatalf("ranking order changed: %#v", got)
	}
	if len(got[0].fallbacks) != 1 || got[0].fallbacks[0].knowledge.ID != "copy" {
		t.Fatalf("duplicate content was not retained as a fallback: %#v", got[0].fallbacks)
	}
}

func TestFilterDataAnalysisCandidatesUsesRequestRelativeScores(t *testing.T) {
	results := []*types.SearchResult{
		{KnowledgeID: "best", Score: 0.6},
		{KnowledgeID: "close", Score: 0.4},
		{KnowledgeID: "tail", Score: 0.2},
	}
	got := filterDataAnalysisCandidatesByRelativeScore(results, nil, nil)
	if len(got) != 2 || got[0].KnowledgeID != "best" || got[1].KnowledgeID != "close" {
		t.Fatalf("unexpected relative-score candidates: %#v", got)
	}

	lowScores := []*types.SearchResult{{KnowledgeID: "one", Score: 0.12}, {KnowledgeID: "two", Score: 0.1}}
	if got := filterDataAnalysisCandidatesByRelativeScore(lowScores, nil, nil); len(got) != 2 {
		t.Fatalf("close low-score candidates were dropped: %#v", got)
	}
}

func TestFilterDataAnalysisCandidatesKeepsExplicitKnowledge(t *testing.T) {
	results := []*types.SearchResult{{KnowledgeID: "explicit", Score: 0.9}, {KnowledgeID: "also-explicit", Score: 0.1}}
	got := filterDataAnalysisCandidatesByRelativeScore(results, []string{"explicit", "also-explicit"}, nil)
	if len(got) != len(results) {
		t.Fatalf("explicit candidates were filtered: %#v", got)
	}
}
