package chatpipeline

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/rerank"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/redis/go-redis/v9"
)

type platformRerankModelService struct {
	interfaces.ModelService
	requestedModelID string
}

func (s *platformRerankModelService) GetRerankModel(_ context.Context, modelID string) (rerank.Reranker, error) {
	s.requestedModelID = modelID
	return nil, errors.New("stop after model resolution")
}

func TestRerankUsesPlatformModelWhenRequestModelIDIsEmpty(t *testing.T) {
	models := &platformRerankModelService{}
	plugin := &PluginRerank{modelService: models}
	manage := &types.ChatManage{
		PipelineState: types.PipelineState{
			RewriteQuery: "query",
			SearchResult: []*types.SearchResult{{ID: "chunk", Content: "content"}},
		},
	}

	nextCalled := false
	if err := plugin.OnEvent(context.Background(), types.CHUNK_RERANK, manage, func() *PluginError { nextCalled = true; return nil }); err != nil {
		t.Fatalf("model unavailability should degrade to raw recall: %v", err)
	}
	if models.requestedModelID != "" {
		t.Fatalf("requested model ID = %q, want platform default", models.requestedModelID)
	}
	if !nextCalled || manage.RerankOutcome != types.RerankOutcomeUnavailable || len(manage.RerankResult) != 1 {
		t.Fatalf("unexpected model-unavailable fallback: next=%v outcome=%q results=%d", nextCalled, manage.RerankOutcome, len(manage.RerankResult))
	}
}

func TestPrepareRerankCandidatesDeduplicatesAndLimits(t *testing.T) {
	results := []*types.SearchResult{
		{ID: "chunk-1", Content: "same content"},
		{ID: "chunk-1", Content: "same content"},
		{ID: "chunk-2", Content: "same content"},
		{ID: "chunk-3", Content: "different content"},
		{ID: "chunk-4", Content: "another content"},
	}

	got := prepareRerankCandidates(results, 2)
	if len(got) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(got))
	}
	if got[0].ID != "chunk-1" || got[1].ID != "chunk-3" {
		t.Fatalf("unexpected candidates after deduplication: %s, %s", got[0].ID, got[1].ID)
	}
}

func TestAdaptiveRerankCandidateLimit(t *testing.T) {
	tests := []struct {
		name   string
		manage *types.ChatManage
		want   int
	}{
		{
			name: "short entity",
			manage: &types.ChatManage{PipelineRequest: types.PipelineRequest{
				EmbeddingTopK: 30,
			}, PipelineState: types.PipelineState{RewriteQuery: "广电计量"}},
			want: 20,
		},
		{
			name: "general question",
			manage: &types.ChatManage{PipelineRequest: types.PipelineRequest{
				EmbeddingTopK: 30,
			}, PipelineState: types.PipelineState{RewriteQuery: "广电计量有哪些主要业务板块"}},
			want: 25,
		},
		{
			name: "complex comparison",
			manage: &types.ChatManage{PipelineRequest: types.PipelineRequest{
				EmbeddingTopK: 30,
			}, PipelineState: types.PipelineState{RewriteQuery: "对比广电计量不同业务板块的区别和优缺点"}},
			want: 30,
		},
		{
			name: "configured maximum wins",
			manage: &types.ChatManage{PipelineRequest: types.PipelineRequest{
				EmbeddingTopK: 10,
			}, PipelineState: types.PipelineState{RewriteQuery: "广电计量"}},
			want: 10,
		},
		{
			name: "explicit rerank candidate limit wins",
			manage: &types.ChatManage{PipelineRequest: types.PipelineRequest{
				EmbeddingTopK:       30,
				RerankCandidateTopK: 12,
			}, PipelineState: types.PipelineState{RewriteQuery: "广电计量有哪些主要业务板块"}},
			want: 12,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := adaptiveRerankCandidateLimit(test.manage); got != test.want {
				t.Fatalf("expected candidate limit %d, got %d", test.want, got)
			}
		})
	}
}

type fakeRerankRedisCache struct {
	mu     sync.Mutex
	values map[string][]byte
}

func (f *fakeRerankRedisCache) Get(_ context.Context, key string) *redis.StringCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	value, ok := f.values[key]
	if !ok {
		return redis.NewStringResult("", redis.Nil)
	}
	return redis.NewStringResult(string(value), nil)
}

func (f *fakeRerankRedisCache) Set(
	_ context.Context,
	key string,
	value interface{},
	_ time.Duration,
) *redis.StatusCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := value.([]byte)
	if !ok {
		return redis.NewStatusResult("", nil)
	}
	f.values[key] = append([]byte(nil), data...)
	return redis.NewStatusResult("OK", nil)
}

type countingReranker struct {
	mu    sync.Mutex
	calls int
}

type fixedRerankModelService struct {
	interfaces.ModelService
	model rerank.Reranker
}

func (s fixedRerankModelService) GetRerankModel(context.Context, string) (rerank.Reranker, error) {
	return s.model, nil
}

type fixedReranker struct {
	results []rerank.RankResult
	err     error
}

func (r fixedReranker) Rerank(context.Context, string, []string) ([]rerank.RankResult, error) {
	return r.results, r.err
}

func (fixedReranker) GetModelName() string { return "fixed-reranker" }
func (fixedReranker) GetModelID() string   { return "fixed-reranker-id" }

func TestRerankEmptyResultIsNoRelevantResult(t *testing.T) {
	plugin := &PluginRerank{modelService: fixedRerankModelService{model: fixedReranker{}}}
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{RerankThreshold: 0.3, RerankTopK: 3},
		PipelineState: types.PipelineState{
			RewriteQuery: "query",
			SearchResult: []*types.SearchResult{{ID: "chunk", Content: "unrelated", MatchType: types.MatchTypeEmbedding}},
		},
	}
	nextCalled := false

	err := plugin.OnEvent(context.Background(), types.CHUNK_RERANK, manage, func() *PluginError {
		nextCalled = true
		return nil
	})
	if err != ErrSearchNothing || manage.RerankOutcome != types.RerankOutcomeNoRelevantResult || nextCalled {
		t.Fatalf("unexpected empty rerank control flow: err=%v outcome=%q next=%v", err, manage.RerankOutcome, nextCalled)
	}
}

func TestRerankUnavailableFallsBackToRawRecall(t *testing.T) {
	plugin := &PluginRerank{modelService: fixedRerankModelService{model: fixedReranker{err: errors.New("service unavailable")}}}
	candidate := &types.SearchResult{ID: "chunk", Content: "candidate", MatchType: types.MatchTypeEmbedding}
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{RerankThreshold: 0.3, RerankTopK: 3},
		PipelineState:   types.PipelineState{RewriteQuery: "query", SearchResult: []*types.SearchResult{candidate}},
	}
	nextCalled := false

	err := plugin.OnEvent(context.Background(), types.CHUNK_RERANK, manage, func() *PluginError {
		nextCalled = true
		return nil
	})
	if err != nil || manage.RerankOutcome != types.RerankOutcomeUnavailable || !nextCalled {
		t.Fatalf("unexpected unavailable control flow: err=%v outcome=%q next=%v", err, manage.RerankOutcome, nextCalled)
	}
	if len(manage.RerankResult) != 1 || manage.RerankResult[0] != candidate {
		t.Fatalf("raw recall was not preserved: %+v", manage.RerankResult)
	}
}

func TestRerankInvalidResponseIndexIsInvalidCandidate(t *testing.T) {
	plugin := &PluginRerank{modelService: fixedRerankModelService{model: fixedReranker{results: []rerank.RankResult{{Index: -1, RelevanceScore: 0.9}}}}}
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{RerankThreshold: 0.3, RerankTopK: 3},
		PipelineState: types.PipelineState{RewriteQuery: "query", SearchResult: []*types.SearchResult{
			{ID: "chunk", Content: "candidate", MatchType: types.MatchTypeEmbedding},
		}},
	}
	nextCalled := false
	err := plugin.OnEvent(context.Background(), types.CHUNK_RERANK, manage, func() *PluginError { nextCalled = true; return nil })
	if err == nil || manage.RerankOutcome != types.RerankOutcomeInvalidCandidate || nextCalled {
		t.Fatalf("invalid rerank response was not rejected: err=%v outcome=%q next=%v", err, manage.RerankOutcome, nextCalled)
	}
}

func TestRerankEmptyPassagesAreInvalidCandidates(t *testing.T) {
	plugin := &PluginRerank{modelService: fixedRerankModelService{model: fixedReranker{}}}
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{RerankThreshold: 0.3, RerankTopK: 3},
		PipelineState: types.PipelineState{RewriteQuery: "query", SearchResult: []*types.SearchResult{
			{ID: "chunk", Content: "", MatchType: types.MatchTypeEmbedding},
		}},
	}
	nextCalled := false
	err := plugin.OnEvent(context.Background(), types.CHUNK_RERANK, manage, func() *PluginError { nextCalled = true; return nil })
	if err == nil || manage.RerankOutcome != types.RerankOutcomeInvalidCandidate || nextCalled {
		t.Fatalf("empty rerank passages were not rejected: err=%v outcome=%q next=%v", err, manage.RerankOutcome, nextCalled)
	}
}

func (r *countingReranker) Rerank(
	_ context.Context,
	_ string,
	_ []string,
) ([]rerank.RankResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return []rerank.RankResult{{Index: 0, RelevanceScore: 0.9}}, nil
}

func (r *countingReranker) GetModelName() string { return "test-reranker" }

func (r *countingReranker) GetModelID() string { return "test-reranker-id" }

func TestRerankWithCacheReusesIdenticalResults(t *testing.T) {
	cache := &fakeRerankRedisCache{values: make(map[string][]byte)}
	model := &countingReranker{}
	plugin := &PluginRerank{cache: cache}
	candidates := []*types.SearchResult{{ID: "chunk-1", Content: "document"}}
	passages := []string{"document"}

	first, err := plugin.rerankWithCache(context.Background(), model, "query", passages, candidates)
	if err != nil {
		t.Fatalf("first rerank: %v", err)
	}
	second, err := plugin.rerankWithCache(context.Background(), model, "query", passages, candidates)
	if err != nil {
		t.Fatalf("second rerank: %v", err)
	}
	if model.calls != 1 {
		t.Fatalf("expected one model call, got %d", model.calls)
	}
	if len(first) != 1 || len(second) != 1 || second[0].RelevanceScore != first[0].RelevanceScore {
		t.Fatalf("unexpected cached results: first=%+v second=%+v", first, second)
	}
}

func TestRerankCacheKeyChangesWithPassageContent(t *testing.T) {
	candidates := []*types.SearchResult{{ID: "chunk-1"}}
	first := buildRerankCacheKey("model", "query", []string{"old content"}, candidates)
	second := buildRerankCacheKey("model", "query", []string{"new content"}, candidates)
	if first == second {
		t.Fatal("expected passage content changes to invalidate the rerank cache key")
	}
}
