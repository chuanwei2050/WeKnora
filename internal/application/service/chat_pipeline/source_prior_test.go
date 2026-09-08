package chatpipeline

import (
	"context"
	"math"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type wikiPriorKBService struct {
	interfaces.KnowledgeBaseService
}

func (wikiPriorKBService) GetKnowledgeBaseByIDOnly(context.Context, string) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{IndexingStrategy: types.IndexingStrategy{WikiEnabled: true}}, nil
}

func TestBoundedSourcePriorPreservesRawScoreAndRange(t *testing.T) {
	result := &types.SearchResult{Score: 0.4}
	applyBoundedSourcePrior(result, 0.5, "wiki")
	if result.Score <= 0.4 || result.Score > 0.48 {
		t.Fatalf("source prior was not bounded: %.4f", result.Score)
	}
	if result.Metadata["raw_relevance_score"] != "0.4000" || result.Metadata["final_ranking_score"] == "" {
		t.Fatalf("score diagnostics are incomplete: %+v", result.Metadata)
	}
}

func TestWikiPriorIsMarkedBeforeRerankNext(t *testing.T) {
	plugin := &PluginWikiBoost{kbService: wikiPriorKBService{}}
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{SearchTargets: types.SearchTargets{{KnowledgeBaseID: "wiki"}}},
		PipelineState:   types.PipelineState{SearchResult: []*types.SearchResult{{ID: "page", KnowledgeBaseID: "wiki", ChunkType: types.ChunkTypeWikiPage}}},
	}
	markedBeforeNext := false
	err := plugin.OnEvent(context.Background(), types.CHUNK_RERANK, manage, func() *PluginError {
		markedBeforeNext = manage.SearchResult[0].RankingSourcePrior == wikiSourcePrior && manage.SearchResult[0].RankingSourcePriorKind == "wiki"
		return nil
	})
	if err != nil || !markedBeforeNext {
		t.Fatalf("wiki prior was not available before rerank: err=%v metadata=%v", err, manage.SearchResult[0].Metadata)
	}
}

func TestWikiPriorOnlyMarksCandidatesFromWikiKnowledgeBase(t *testing.T) {
	plugin := &PluginWikiBoost{kbService: wikiPriorKBService{}}
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{SearchTargets: types.SearchTargets{{KnowledgeBaseID: "wiki"}}},
		PipelineState: types.PipelineState{SearchResult: []*types.SearchResult{
			nil,
			{ID: "wiki-page", KnowledgeBaseID: "wiki", ChunkType: types.ChunkTypeWikiPage},
			{ID: "foreign-page", KnowledgeBaseID: "ordinary", ChunkType: types.ChunkTypeWikiPage},
		}},
	}
	if err := plugin.OnEvent(context.Background(), types.CHUNK_RERANK, manage, func() *PluginError { return nil }); err != nil {
		t.Fatal(err)
	}
	if manage.SearchResult[1].RankingSourcePrior != wikiSourcePrior || manage.SearchResult[2].RankingSourcePrior != 0 {
		t.Fatalf("wiki prior crossed KB boundary: %+v", manage.SearchResult)
	}
}

func TestBoundedSourcePriorRejectsNonFiniteAndUntrustedMetadata(t *testing.T) {
	result := &types.SearchResult{Score: 0.4, Metadata: map[string]string{"source_prior": "NaN"}}
	applyBoundedSourcePrior(result, math.NaN(), "wiki")
	if result.Score != 0.4 {
		t.Fatalf("non-finite prior changed score: %v", result.Score)
	}
	if _, ok := result.Metadata["ranking_source_prior"]; ok {
		t.Fatal("untrusted metadata was treated as a ranking prior")
	}
}

func TestBoundedSourcePriorCannotOvertakeMateriallyStrongerResult(t *testing.T) {
	wiki := &types.SearchResult{Score: 0.4}
	applyBoundedSourcePrior(wiki, 0.08, "wiki")
	ordinary := &types.SearchResult{Score: 0.55}
	if wiki.Score >= ordinary.Score {
		t.Fatalf("source prior overturned materially stronger relevance: wiki=%.4f ordinary=%.4f", wiki.Score, ordinary.Score)
	}
}

func TestBoundedSourcePriorClampsFinalScore(t *testing.T) {
	result := &types.SearchResult{Score: 2}
	applyBoundedSourcePrior(result, 0.08, "wiki")
	if result.Score != 1 {
		t.Fatalf("final score escaped normalized range: %v", result.Score)
	}
}

func TestConfiguredRerankPriorOnlyChangesNearTies(t *testing.T) {
	manage := &types.ChatManage{PipelineRequest: types.PipelineRequest{FAQPriorityEnabled: true, FAQScoreBoost: 2}}
	faq := &types.SearchResult{Score: 0.8, ChunkType: string(types.ChunkTypeFAQ)}
	applyConfiguredRerankPrior(faq, manage)
	if faq.Score <= 0.8 || faq.Score >= 0.81 {
		t.Fatalf("FAQ prior should be positive and capped below one point: %.4f", faq.Score)
	}
	if faq.Score >= 0.82 {
		t.Fatalf("FAQ prior overturned a materially stronger result: %.4f", faq.Score)
	}

	wiki := &types.SearchResult{Score: 0.8, RankingSourcePrior: wikiSourcePrior, RankingSourcePriorKind: "wiki"}
	applyConfiguredRerankPrior(wiki, &types.ChatManage{})
	if wiki.Score != faq.Score {
		t.Fatalf("configured priors should share the same cap: wiki=%.4f faq=%.4f", wiki.Score, faq.Score)
	}
}
