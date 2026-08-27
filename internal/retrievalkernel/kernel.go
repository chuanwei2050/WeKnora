package retrievalkernel

import (
	"context"
	"fmt"
	"sort"
	"sync/atomic"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	MaxSearchTargetsPerRequest = 64
	MaxCandidatesPerRequest    = 500
	MaxRecallPerChannel        = 500
)

func BoundCandidateLimit(limit int) int {
	if limit < 1 {
		return types.DefaultEmbeddingTopK
	}
	return min(limit, MaxCandidatesPerRequest)
}

func BoundRecallLimit(limit int) int {
	if limit < 1 {
		return MaxRecallPerChannel
	}
	return min(limit, MaxRecallPerChannel)
}

func ValidateTargetBounds(targets types.SearchTargets, candidateLimit int) error {
	if len(targets) > MaxSearchTargetsPerRequest {
		return fmt.Errorf("search target count %d exceeds limit %d", len(targets), MaxSearchTargetsPerRequest)
	}
	candidateLimit = BoundCandidateLimit(candidateLimit)
	explicit := make(map[string]struct{})
	for _, target := range targets {
		if target == nil || target.Type != types.SearchTargetTypeKnowledge {
			continue
		}
		for _, id := range target.KnowledgeIDs {
			if id != "" {
				explicit[id] = struct{}{}
			}
		}
	}
	if len(explicit) > candidateLimit {
		return fmt.Errorf("explicit knowledge count %d exceeds candidate limit %d", len(explicit), candidateLimit)
	}
	return nil
}

type Outcome string

const (
	OutcomeSuccess          Outcome = "success"
	OutcomeNoRelevantResult Outcome = "no_relevant_result"
	OutcomeUnavailable      Outcome = "unavailable"
	OutcomeInvalidCandidate Outcome = "invalid_candidate"
)

func ClassifyRerank(resultCount int, err error, hasValidCandidates bool) Outcome {
	if !hasValidCandidates {
		return OutcomeInvalidCandidate
	}
	if err != nil {
		return OutcomeUnavailable
	}
	if resultCount == 0 {
		return OutcomeNoRelevantResult
	}
	return OutcomeSuccess
}

type Limiter struct {
	permits       chan struct{}
	waitNanos     atomic.Int64
	cancellations atomic.Int64
}

type limiterContextKey struct{}

func WithLimiter(ctx context.Context, limiter *Limiter) context.Context {
	return context.WithValue(ctx, limiterContextKey{}, limiter)
}

func LimiterFromContext(ctx context.Context, fallback int) *Limiter {
	if limiter, ok := ctx.Value(limiterContextKey{}).(*Limiter); ok && limiter != nil {
		return limiter
	}
	return NewLimiter(fallback)
}

func NewLimiter(limit int) *Limiter {
	if limit < 1 {
		limit = 1
	}
	return &Limiter{permits: make(chan struct{}, limit)}
}

func (l *Limiter) Acquire(ctx context.Context) bool {
	started := time.Now()
	defer func() { l.waitNanos.Add(time.Since(started).Nanoseconds()) }()
	if ctx.Err() != nil {
		l.cancellations.Add(1)
		return false
	}
	select {
	case l.permits <- struct{}{}:
		if ctx.Err() == nil {
			return true
		}
		l.Release()
		l.cancellations.Add(1)
		return false
	case <-ctx.Done():
		l.cancellations.Add(1)
		return false
	}
}

func (l *Limiter) Stats() (wait time.Duration, cancellations int64) {
	return time.Duration(l.waitNanos.Load()), l.cancellations.Load()
}

func (l *Limiter) Release() {
	<-l.permits
}

type TargetPlan struct {
	Groups        map[string][]*types.SearchTarget
	GroupKeys     []string
	TasksPerQuery int
}

func PlanTargets(targets types.SearchTargets, modelKeys map[string]string) TargetPlan {
	plan := TargetPlan{Groups: make(map[string][]*types.SearchTarget)}
	for _, target := range targets {
		if target == nil {
			continue
		}
		key := modelKeys[target.KnowledgeBaseID]
		plan.Groups[key] = append(plan.Groups[key], target)
	}
	for key, group := range plan.Groups {
		plan.GroupKeys = append(plan.GroupKeys, key)
		hasFullKB := false
		for _, target := range group {
			if target.Type == types.SearchTargetTypeKnowledgeBase {
				hasFullKB = true
			} else {
				plan.TasksPerQuery++
			}
		}
		if hasFullKB {
			plan.TasksPerQuery++
		}
	}
	sort.Strings(plan.GroupKeys)
	return plan
}
