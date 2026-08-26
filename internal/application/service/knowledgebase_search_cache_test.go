package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/redis/go-redis/v9"
)

type queryEmbeddingCacheStub struct {
	values map[string]string
}

func (s *queryEmbeddingCacheStub) Get(_ context.Context, key string) *redis.StringCmd {
	value, ok := s.values[key]
	if !ok {
		return redis.NewStringResult("", redis.Nil)
	}
	return redis.NewStringResult(value, nil)
}

func (s *queryEmbeddingCacheStub) Set(_ context.Context, key string, value interface{}, _ time.Duration) *redis.StatusCmd {
	bytes, ok := value.([]byte)
	if !ok {
		return redis.NewStatusResult("", errors.New("expected byte cache value"))
	}
	s.values[key] = string(bytes)
	return redis.NewStatusResult("OK", nil)
}

func TestQueryEmbeddingCacheRoundTrip(t *testing.T) {
	cache := &queryEmbeddingCacheStub{values: map[string]string{}}
	service := &knowledgeBaseService{embeddingCache: cache}
	want := []float32{0.1, -0.25, 1.5}

	service.storeQueryEmbeddingCache(context.Background(), "key", want)
	got, ok := service.loadQueryEmbeddingCache(context.Background(), "key")
	if !ok || len(got) != len(want) {
		t.Fatalf("cache round trip failed: ok=%v got=%v", ok, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("embedding changed at %d: got %v want %v", i, got[i], want[i])
		}
	}
}

func TestQueryEmbeddingCacheKeyChangesWithModelConfiguration(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 7, EmbeddingModelID: "model"}
	model := &types.Model{ID: "model", Name: "embed", UpdatedAt: time.Unix(1, 0)}
	first := queryEmbeddingCacheKey(kb, model, "same query")
	model.UpdatedAt = time.Unix(2, 0)
	second := queryEmbeddingCacheKey(kb, model, "same query")
	if first == second {
		t.Fatal("model configuration change must invalidate cached embeddings")
	}
}
