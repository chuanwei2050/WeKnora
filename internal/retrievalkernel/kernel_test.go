package retrievalkernel

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestClassifyRerank(t *testing.T) {
	if got := ClassifyRerank(0, nil, true); got != OutcomeNoRelevantResult {
		t.Fatalf("empty successful rerank = %q", got)
	}
	if got := ClassifyRerank(0, errors.New("down"), true); got != OutcomeUnavailable {
		t.Fatalf("failed rerank = %q", got)
	}
	if got := ClassifyRerank(1, nil, false); got != OutcomeInvalidCandidate {
		t.Fatalf("invalid candidates = %q", got)
	}
}

func TestLimiterCapsConcurrentWork(t *testing.T) {
	limiter := NewLimiter(2)
	var active atomic.Int64
	var peak atomic.Int64
	release := make(chan struct{})
	started := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !limiter.Acquire(context.Background()) {
				return
			}
			current := active.Add(1)
			for old := peak.Load(); current > old && !peak.CompareAndSwap(old, current); old = peak.Load() {
			}
			started <- struct{}{}
			<-release
			active.Add(-1)
			limiter.Release()
		}()
	}
	<-started
	<-started
	if got := peak.Load(); got != 2 {
		t.Fatalf("peak concurrency = %d, want 2", got)
	}
	close(release)
	wg.Wait()
}

func TestPlanTargetsGroupsModelsAndCountsCombinedKBTask(t *testing.T) {
	targets := types.SearchTargets{
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-a"},
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-b"},
		{Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "kb-a", KnowledgeIDs: []string{"doc"}},
	}
	plan := PlanTargets(targets, map[string]string{"kb-a": "model", "kb-b": "model"})
	if len(plan.GroupKeys) != 1 || plan.TasksPerQuery != 2 {
		t.Fatalf("unexpected target plan: %+v", plan)
	}
}

func TestLimiterHonorsCancellation(t *testing.T) {
	limiter := NewLimiter(1)
	if !limiter.Acquire(context.Background()) {
		t.Fatal("initial acquire failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if limiter.Acquire(ctx) {
		t.Fatal("canceled acquire succeeded")
	}
	limiter.Release()
}

func TestLimiterFromContextSharesRoundBudget(t *testing.T) {
	want := NewLimiter(2)
	ctx := WithLimiter(context.Background(), want)
	if got := LimiterFromContext(ctx, 4); got != want {
		t.Fatal("request-scoped limiter was not reused")
	}
}

func TestResourceLimitsClampLegacyValues(t *testing.T) {
	if got := BoundCandidateLimit(10_000); got != MaxCandidatesPerRequest {
		t.Fatalf("candidate limit = %d", got)
	}
	if got := BoundRecallLimit(10_000); got != MaxRecallPerChannel {
		t.Fatalf("recall limit = %d", got)
	}
}
