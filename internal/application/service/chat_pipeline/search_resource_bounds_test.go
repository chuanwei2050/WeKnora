package chatpipeline

import (
	"context"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/retrievalkernel"
	"github.com/Tencent/WeKnora/internal/types"
)

func TestRetrievalLimiterStopsWaitingAfterCancellation(t *testing.T) {
	limiter := retrievalkernel.NewLimiter(1)
	if !limiter.Acquire(context.Background()) {
		t.Fatal("failed to acquire initial permit")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if limiter.Acquire(ctx) {
		t.Fatal("acquired a permit after context cancellation")
	}
	limiter.Release()
}

func TestLimitRetrievalCandidatesPreservesExplicitScopes(t *testing.T) {
	targets := []*types.SearchTarget{{
		Type:         types.SearchTargetTypeKnowledge,
		KnowledgeIDs: []string{"knowledge-a", "knowledge-b"},
	}}
	results := []*types.SearchResult{
		{ID: "a", KnowledgeID: "knowledge-a", Score: 0.1},
		{ID: "b", KnowledgeID: "knowledge-b", Score: 0.2},
		{ID: "other", KnowledgeID: "knowledge-c", Score: 0.9},
	}

	limited := limitRetrievalCandidates(results, 2, targets)
	if len(limited) != 2 || limited[0].KnowledgeID != "knowledge-a" || limited[1].KnowledgeID != "knowledge-b" {
		t.Fatalf("explicit scopes were not preserved: %+v", limited)
	}
}

func TestValidateTargetBoundsRejectsExplicitScopesBeyondFixedBudget(t *testing.T) {
	targets := types.SearchTargets{{Type: types.SearchTargetTypeKnowledge, KnowledgeIDs: []string{"a", "b"}}}
	if err := retrievalkernel.ValidateTargetBounds(targets, 1); err == nil {
		t.Fatal("expected explicit scope count above the fixed candidate budget to be rejected")
	}
}

func TestLimitRetrievalCandidatesCapsAndRanksRemainingResults(t *testing.T) {
	results := []*types.SearchResult{
		{ID: "low", Score: 0.1},
		{ID: "high", Score: 0.9},
		{ID: "mid", Score: 0.5},
	}
	limited := limitRetrievalCandidates(results, 2, nil)
	if len(limited) != 2 || limited[0].ID != "high" || limited[1].ID != "mid" {
		t.Fatalf("unexpected candidate cap result: %+v", limited)
	}
}

func TestLimitRetrievalCandidatesKeepsHighestDuplicateAndEachExplicitScope(t *testing.T) {
	targets := types.SearchTargets{{Type: types.SearchTargetTypeKnowledge, KnowledgeIDs: []string{"a", "b"}}}
	results := []*types.SearchResult{
		{ID: "same-id", KnowledgeID: "a", Content: "shared", Score: 0.1},
		{ID: "same-id", KnowledgeID: "a", Content: "shared", Score: 0.9},
		{ID: "b-id", KnowledgeID: "b", Content: "shared", Score: 0.2},
	}
	limited := limitRetrievalCandidates(results, 2, targets)
	if len(limited) != 2 || limited[0].KnowledgeID != "a" || limited[0].Score != 0.9 || limited[1].KnowledgeID != "b" {
		t.Fatalf("explicit scope fairness or duplicate score selection failed: %+v", limited)
	}
}

func TestRemoveDuplicateResultsKeepsHighestContentDuplicateDeterministically(t *testing.T) {
	results := []*types.SearchResult{
		{ID: "low", KnowledgeID: "a", Content: "same content", Score: 0.1},
		{ID: "high", KnowledgeID: "a", Content: "same content", Score: 0.9},
	}
	got := removeDuplicateResults(results)
	if len(got) != 1 || got[0].ID != "high" {
		t.Fatalf("content dedup kept the wrong candidate: %+v", got)
	}
}

func TestDirectLoadTurnWaitHonorsCancellation(t *testing.T) {
	budget := newDirectLoadBudget()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if budget.waitTurn(ctx, 1) {
		t.Fatal("canceled later turn acquired the direct-load budget")
	}
}

func TestLimitRetrievalCandidatesCountsDirectLoadAgainstHardLimit(t *testing.T) {
	results := make([]*types.SearchResult, 0, retrievalkernel.MaxCandidatesPerRequest+50)
	for i := 0; i < 50; i++ {
		results = append(results, &types.SearchResult{
			ID: fmt.Sprintf("direct-%d", i), KnowledgeID: "explicit", ChunkIndex: i,
			MatchType: types.MatchTypeDirectLoad, Metadata: map[string]string{"direct_load_reason": "full_document_intent"},
		})
	}
	for i := 0; i < retrievalkernel.MaxCandidatesPerRequest; i++ {
		results = append(results, &types.SearchResult{ID: fmt.Sprintf("regular-%d", i), Score: float64(i)})
	}
	targets := types.SearchTargets{{Type: types.SearchTargetTypeKnowledge, KnowledgeIDs: []string{"explicit"}}}
	got := limitRetrievalCandidates(results, retrievalkernel.MaxCandidatesPerRequest, targets)
	if len(got) != retrievalkernel.MaxCandidatesPerRequest {
		t.Fatalf("hard candidate limit exceeded: %d", len(got))
	}
}
