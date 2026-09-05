package chatpipeline

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestMMRCanUseRelevantDiverseCandidateBeyondRawTopK(t *testing.T) {
	results := []*types.SearchResult{
		{ID: "first", Content: "alpha beta", Score: 0.90},
		{ID: "duplicate", Content: "alpha beta", Score: 0.80},
		{ID: "diverse", Content: "gamma delta", Score: 0.75},
	}

	got := applyMMR(context.Background(), results, &types.ChatManage{}, 2, mmrRelevanceWeight)
	if len(got) != 2 || got[0].ID != "first" || got[1].ID != "diverse" {
		t.Fatalf("expected relevant diverse result to supplement raw Top-K: %+v", got)
	}
}

func TestMMRDoesNotLetDiversityPromoteWeakTail(t *testing.T) {
	results := []*types.SearchResult{
		{ID: "first", Content: "alpha beta", Score: 0.90},
		{ID: "second", Content: "alpha beta", Score: 0.80},
		{ID: "weak-diverse", Content: "gamma delta", Score: 0.40},
	}

	got := applyMMR(context.Background(), results, &types.ChatManage{}, 2, mmrRelevanceWeight)
	if len(got) != 2 {
		t.Fatalf("unexpected MMR result count: %d", len(got))
	}
	for _, result := range got {
		if result.ID == "weak-diverse" {
			t.Fatalf("MMR promoted a materially weaker result: %+v", got)
		}
	}
}
