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

func TestPreserveAcceptedKeywordLeaderAfterMMR(t *testing.T) {
	semantic := &types.SearchResult{ID: "semantic", Score: 0.95}
	leader := &types.SearchResult{ID: "leader", Score: 0.8, KeywordLeader: true}
	diverse := &types.SearchResult{ID: "diverse", Score: 0.7}

	got := ensureAcceptedKeywordLeader(
		[]*types.SearchResult{semantic, diverse},
		[]*types.SearchResult{semantic, leader, diverse},
		2,
	)
	if len(got) != 2 || got[0].ID != "semantic" || got[1].ID != "leader" {
		t.Fatalf("accepted keyword leader was not retained: %+v", got)
	}
	if leader.Score != 0.8 {
		t.Fatalf("keyword leader score changed: %v", leader.Score)
	}
}

func TestPreserveAcceptedKeywordLeaderDoesNotAddRejectedCandidate(t *testing.T) {
	semantic := &types.SearchResult{ID: "semantic", Score: 0.95}
	rejected := &types.SearchResult{ID: "rejected", Score: 0.1, KeywordLeader: true}

	got := ensureAcceptedKeywordLeader(
		[]*types.SearchResult{semantic},
		[]*types.SearchResult{semantic},
		2,
	)
	if len(got) != 1 || got[0].ID != "semantic" || rejected.Score != 0.1 {
		t.Fatalf("rejected keyword candidate was restored: %+v", got)
	}
}

func TestEnsureAcceptedKeywordLeaderDoesNotDisplaceAllMMRResults(t *testing.T) {
	semantic := &types.SearchResult{ID: "semantic", Score: 0.95}
	diverse := &types.SearchResult{ID: "diverse", Score: 0.9}
	leader1 := &types.SearchResult{ID: "leader-1", Score: 0.8, KeywordLeader: true}
	leader2 := &types.SearchResult{ID: "leader-2", Score: 0.7, KeywordLeader: true}

	got := ensureAcceptedKeywordLeader(
		[]*types.SearchResult{semantic, diverse},
		[]*types.SearchResult{semantic, diverse, leader1, leader2},
		2,
	)
	if len(got) != 2 || got[0].ID != "semantic" || got[1].ID != "leader-1" {
		t.Fatalf("keyword leaders displaced all MMR results: %+v", got)
	}
}
