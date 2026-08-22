package utils

import (
	"os"
	"strconv"
)

const (
	defaultConcurrencyPoolSize = 5
	defaultBatchEmbedSize      = 20
	defaultAsynqConcurrency    = 12
)

// ConcurrencyPoolSize returns the ants goroutine-pool size used for embedding
// batches and other CPU/IO-bound fan-out work.
func ConcurrencyPoolSize() int {
	return intFromEnv("CONCURRENCY_POOL_SIZE", defaultConcurrencyPoolSize, 1, 64)
}

// BatchEmbedSize returns how many texts are grouped per embedding API call
// inside BatchEmbedWithPool.
func BatchEmbedSize() int {
	return intFromEnv("BATCH_EMBED_SIZE", defaultBatchEmbedSize, 1, 128)
}

// AsynqConcurrency returns the number of concurrent asynq task workers.
func AsynqConcurrency() int {
	return intFromEnv("ASYNQ_CONCURRENCY", defaultAsynqConcurrency, 1, 128)
}

func intFromEnv(key string, defaultVal, minVal, maxVal int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return clampInt(defaultVal, minVal, maxVal)
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return clampInt(defaultVal, minVal, maxVal)
	}
	return clampInt(parsed, minVal, maxVal)
}

func clampInt(value, minVal, maxVal int) int {
	if value < minVal {
		return minVal
	}
	if value > maxVal {
		return maxVal
	}
	return value
}
