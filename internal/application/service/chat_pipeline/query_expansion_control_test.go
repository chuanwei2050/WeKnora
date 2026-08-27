package chatpipeline

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/retrievalkernel"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

func TestShouldExpandQueryUsesQualityAndRoutingBudget(t *testing.T) {
	highQuality := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{EnableQueryExpansion: true, EmbeddingTopK: 10},
		PipelineState:   types.PipelineState{SearchResult: []*types.SearchResult{{Score: 0.8}}},
	}
	if shouldExpandQuery(highQuality) {
		t.Fatal("small high-quality recall should not trigger expansion")
	}
	highQuality.SearchResult = []*types.SearchResult{{Score: 1.0 / 61, ScoreDomain: types.RetrievalScoreDomainRRF}}
	if shouldExpandQuery(highQuality) {
		t.Fatal("a typed high-quality RRF score should be normalized before expansion gating")
	}

	lowQuality := highQuality.Clone()
	lowQuality.SearchResult = []*types.SearchResult{{Score: 0.2}}
	if !shouldExpandQuery(lowQuality) {
		t.Fatal("low-quality recall should trigger expansion")
	}

	lowQuality.RoutingDecision = &types.RoutingDecision{Budget: types.RoutingBudget{QueryExpansion: false}}
	if shouldExpandQuery(lowQuality) {
		t.Fatal("routing budget should disable expansion")
	}
}

type oversizedExpansionKBService struct {
	interfaces.KnowledgeBaseService
}

func (oversizedExpansionKBService) HybridSearch(context.Context, string, types.SearchParams) ([]*types.SearchResult, error) {
	results := make([]*types.SearchResult, maxExpansionGovernanceScan+500)
	for i := range results {
		results[i] = &types.SearchResult{ID: "candidate", KnowledgeID: "missing"}
	}
	results[maxExpansionGovernanceScan-1].KnowledgeID = "valid"
	return results, nil
}

type expansionKnowledgeService struct {
	interfaces.KnowledgeService
}

func (expansionKnowledgeService) GetKnowledgeBatchWithSharedAccess(context.Context, uint64, []string) ([]*types.Knowledge, error) {
	return []*types.Knowledge{{ID: "valid", KnowledgeBaseID: "kb"}}, nil
}

func TestExpansionGovernsBeforeApplyingHardCandidateBudget(t *testing.T) {
	plugin := &PluginSearch{knowledgeBaseService: oversizedExpansionKBService{}, knowledgeService: expansionKnowledgeService{}}
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			TenantID: 7, EmbeddingTopK: 10, VectorRecallTopK: 10, KeywordRecallTopK: 10,
			SearchTargets: types.SearchTargets{{KnowledgeBaseID: "kb"}},
		},
		PipelineState: types.PipelineState{RewriteQuery: "请告诉我软件测试治理如何工作"},
	}
	results := plugin.runQueryExpansion(t.Context(), manage, retrievalkernel.NewLimiter(1))
	if len(results) == 0 {
		t.Fatal("valid candidate was lost by truncation before governance")
	}
	if len(results) > expansionCandidateLimit(manage) {
		t.Fatalf("expansion exceeded hard candidate budget: %d", len(results))
	}
	for _, result := range results {
		if result.KnowledgeID != "valid" {
			t.Fatalf("ungoverned candidate survived: %+v", result)
		}
	}
}

func TestShouldExpandQueryIgnoresNilCandidates(t *testing.T) {
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{EnableQueryExpansion: true, EmbeddingTopK: 2},
		PipelineState:   types.PipelineState{SearchResult: []*types.SearchResult{nil, {Score: 0.8}}},
	}
	if shouldExpandQuery(manage) {
		t.Fatal("nil candidates should not hide a high-quality recall")
	}
	manage.SearchResult = []*types.SearchResult{nil}
	if !shouldExpandQuery(manage) {
		t.Fatal("an all-nil recall should trigger expansion")
	}
}

func TestTokenizeUsesWordSegmentsForChinese(t *testing.T) {
	tokens := tokenize("软件测试知识治理")
	for _, token := range tokens {
		if len([]rune(token)) > 1 {
			return
		}
	}
	t.Fatalf("Chinese query was split only into individual characters: %v", tokens)
}

func TestExpandQueriesCapsVariants(t *testing.T) {
	plugin := &PluginSearch{}
	manage := &types.ChatManage{PipelineState: types.PipelineState{
		RewriteQuery: "请告诉我「软件测试治理」如何工作，以及版本发布；还要说明检索规则",
	}}
	if got := plugin.expandQueries(t.Context(), manage); len(got) > 3 {
		t.Fatalf("expansion variants exceeded request budget: %v", got)
	}
}

func TestExpansionCallBudgetIsBounded(t *testing.T) {
	jobs := min(maxQueryExpansionVariants*64, maxQueryExpansionCalls)
	if jobs != maxQueryExpansionCalls {
		t.Fatalf("expansion call budget = %d", jobs)
	}
}

func TestExpansionCandidateBudgetClampsOversizedRerankConfig(t *testing.T) {
	manage := &types.ChatManage{PipelineRequest: types.PipelineRequest{EmbeddingTopK: 10, RerankTopK: 1_000_000}}
	if got := expansionCandidateLimit(manage); got != retrievalkernel.MaxCandidatesPerRequest {
		t.Fatalf("expansion candidate limit = %d", got)
	}
}
