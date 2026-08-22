package utils

import (
	"testing"
)

func TestConcurrencyPoolSizeDefault(t *testing.T) {
	t.Setenv("CONCURRENCY_POOL_SIZE", "")
	if got := ConcurrencyPoolSize(); got != defaultConcurrencyPoolSize {
		t.Fatalf("ConcurrencyPoolSize() = %d, want %d", got, defaultConcurrencyPoolSize)
	}
}

func TestBatchEmbedSizeClampsInvalid(t *testing.T) {
	t.Setenv("BATCH_EMBED_SIZE", "0")
	if got := BatchEmbedSize(); got != 1 {
		t.Fatalf("BatchEmbedSize() = %d, want 1", got)
	}
}

func TestAsynqConcurrencyFromEnv(t *testing.T) {
	t.Setenv("ASYNQ_CONCURRENCY", "16")
	if got := AsynqConcurrency(); got != 16 {
		t.Fatalf("AsynqConcurrency() = %d, want 16", got)
	}
}
