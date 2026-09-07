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
	got := filterDataAnalysisCandidatesByRelativeScore(results, nil, nil, "普通统计问题")
	if len(got) != 1 || got[0].KnowledgeID != "best" {
		t.Fatalf("unexpected relative-score candidates: %#v", got)
	}

	lowScores := []*types.SearchResult{{KnowledgeID: "one", Score: 0.12}, {KnowledgeID: "two", Score: 0.09}}
	if got := filterDataAnalysisCandidatesByRelativeScore(lowScores, nil, nil, "普通统计问题"); len(got) != 1 {
		t.Fatalf("weak second candidate was retained: %#v", got)
	}

	nearTie := []*types.SearchResult{{KnowledgeID: "one", Score: 0.6}, {KnowledgeID: "two", Score: 0.58}}
	if got := filterDataAnalysisCandidatesByRelativeScore(nearTie, nil, nil, "普通统计问题"); len(got) != 2 {
		t.Fatalf("strong second candidate was dropped: %#v", got)
	}

	threeWayTie := []*types.SearchResult{{KnowledgeID: "one", Score: 0.6}, {KnowledgeID: "two", Score: 0.59}, {KnowledgeID: "three", Score: 0.588}}
	if got := filterDataAnalysisCandidatesByRelativeScore(threeWayTie, nil, nil, "普通统计问题"); len(got) != 3 {
		t.Fatalf("strong third candidate was dropped: %#v", got)
	}
}

func TestFilterDataAnalysisCandidatesKeepsExplicitKnowledge(t *testing.T) {
	results := []*types.SearchResult{{KnowledgeID: "explicit", Score: 0.9}, {KnowledgeID: "also-explicit", Score: 0.1}}
	got := filterDataAnalysisCandidatesByRelativeScore(results, []string{"explicit", "also-explicit"}, nil, "query")
	if len(got) != len(results) {
		t.Fatalf("explicit candidates were filtered: %#v", got)
	}
}

func TestFilterDataAnalysisCandidatesLimitsObservedThreeTableSelections(t *testing.T) {
	tests := []struct {
		name   string
		scores []float64
		want   int
	}{
		{name: "a strong runner-up expands without admitting the third", scores: []float64{0.6354179212, 0.5232272959, 0.5577818713}, want: 2},
		{name: "ordinary score gaps stay on one table", scores: []float64{0.6948359011, 0.5403284500, 0.5151196010}, want: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			results := make([]*types.SearchResult, 0, len(test.scores))
			for index, score := range test.scores {
				results = append(results, &types.SearchResult{KnowledgeID: string(rune('a' + index)), Score: score})
			}
			if got := filterDataAnalysisCandidatesByRelativeScore(results, nil, nil, "普通统计问题"); len(got) != test.want {
				t.Fatalf("selected %d candidates, want %d: %#v", len(got), test.want, got)
			}
		})
	}
}

func TestFilterDataAnalysisCandidatesExpandsForDistinctQueryCoverage(t *testing.T) {
	query := "软件评测师、计算机软件产品检验员或ISTQB证书分别多少人"
	results := []*types.SearchResult{
		{KnowledgeID: "first", Score: 0.8, Content: "专业证书：软件评测师"},
		{KnowledgeID: "second", Score: 0.4, Content: "证书名称：计算机软件产品检验员"},
		{KnowledgeID: "third", Score: 0.2, Content: "认证类型：ISTQB"},
	}
	got := filterDataAnalysisCandidatesByRelativeScore(results, nil, nil, query)
	if len(got) != 3 {
		t.Fatalf("distinct requested categories were dropped: %#v", got)
	}
}

func TestDataAnalysisHasMultipleRequestedTargetsRecognizesCommonSeparators(t *testing.T) {
	for _, query := range []string{"分别统计A和B人数", "软件测评师, ISTQB证书人数", "A与B人数", "A或B", "A、B", "A/B", "A and B", "A or B"} {
		if !dataAnalysisHasMultipleRequestedTargets(query) {
			t.Fatalf("multi-target query was not recognized: %q", query)
		}
	}
	for _, query := range []string{"系统集成项目管理工程师证书人员", "查询人员姓名和证书编号", "输出姓名,工号"} {
		if dataAnalysisHasMultipleRequestedTargets(query) {
			t.Fatalf("single-dataset query was recognized as multi-target: %q", query)
		}
	}
}

func TestFilterDataAnalysisCandidatesDoesNotExpandWithoutCoverageGain(t *testing.T) {
	query := "系统架构设计师证书人员是谁"
	results := []*types.SearchResult{
		{KnowledgeID: "first", Score: 0.8, Content: "系统架构设计师：麦伟华"},
		{KnowledgeID: "second", Score: 0.61, Content: "软件测试人员清单"},
		{KnowledgeID: "third", Score: 0.6, Content: "项目管理人员清单"},
	}
	got := filterDataAnalysisCandidatesByRelativeScore(results, nil, nil, query)
	if len(got) != 1 {
		t.Fatalf("candidates without new query coverage were retained: %#v", got)
	}
}

func TestFilterDataAnalysisCandidatesDoesNotUseCoverageForSingleTarget(t *testing.T) {
	query := "请重新核对并列出持有系统集成项目管理工程师证书的人员"
	results := []*types.SearchResult{
		{KnowledgeID: "first", Score: 0.7, Content: "专业证书：系统集成项目管理工程师"},
		{KnowledgeID: "second", Score: 0.67, Content: "职称专业：系统集成项目管理工程师"},
		{KnowledgeID: "third", Score: 0.59, Content: "系统集成项目管理工程师证书人员名单"},
	}
	got := filterDataAnalysisCandidatesByRelativeScore(results, nil, nil, query)
	if len(got) != 2 {
		t.Fatalf("single-target query selected %d candidates, want 2: %#v", len(got), got)
	}
}

func TestFilterDataAnalysisCandidatesKeepsOneForNonPositiveScores(t *testing.T) {
	for _, scores := range [][]float64{{0, 0, 0}, {-0.1, -0.2, -0.3}} {
		results := make([]*types.SearchResult, 0, len(scores))
		for index, score := range scores {
			results = append(results, &types.SearchResult{KnowledgeID: string(rune('a' + index)), Score: score})
		}
		got := filterDataAnalysisCandidatesByRelativeScore(results, nil, nil, "query")
		if len(got) != 1 || got[0].KnowledgeID != "a" {
			t.Fatalf("scores=%v selected unexpected candidates: %#v", scores, got)
		}
	}
}
