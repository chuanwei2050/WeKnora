package chatpipeline

import (
	"context"
	"github.com/Tencent/WeKnora/internal/models/rerank"
	"github.com/Tencent/WeKnora/internal/types"
	"testing"
)

func TestRerankKeywordLeaderCannotOverrideModelRejection(t *testing.T) {
	for _, onlyKeyword := range []bool{false, true} {
		t.Run(map[bool]string{false: "retain_semantic", true: "reject_all"}[onlyKeyword], func(t *testing.T) {
			candidates := []*types.SearchResult{{ID: "keyword", Content: "unrelated lexical hit", Score: 0.001, KeywordLeader: true}}
			scores := []rerank.RankResult{{Index: 0, RelevanceScore: 0.01}}
			if !onlyKeyword {
				candidates = append(candidates, &types.SearchResult{ID: "semantic", Content: "correct answer", Score: 0.9})
				scores = append(scores, rerank.RankResult{Index: 1, RelevanceScore: 0.99})
			}
			plugin := &PluginRerank{modelService: fixedRerankModelService{model: fixedReranker{results: scores}}}
			manage := &types.ChatManage{PipelineRequest: types.PipelineRequest{RerankTopK: 1, RerankThreshold: 0.5}, PipelineState: types.PipelineState{RewriteQuery: "query", SearchResult: candidates}}
			err := plugin.OnEvent(context.Background(), types.CHUNK_RERANK, manage, func() *PluginError { return nil })
			if onlyKeyword {
				if err != ErrSearchNothing || len(manage.RerankResult) != 0 {
					t.Fatalf("rejected leader restored: %v %+v", err, manage.RerankResult)
				}
			} else if err != nil || len(manage.RerankResult) != 1 || manage.RerankResult[0].ID != "semantic" {
				t.Fatalf("semantic result displaced: %v %+v", err, manage.RerankResult)
			}
			if candidates[0].Score != 0.001 {
				t.Fatal("keyword score was inflated")
			}
		})
	}
}
