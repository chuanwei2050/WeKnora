package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/redis/go-redis/v9"
)

const graphRebuildTTL = 7 * 24 * time.Hour

// GraphRebuildProgressStore tracks async graph rebuild progress in Redis.
type GraphRebuildProgressStore struct {
	redis *redis.Client
}

func NewGraphRebuildProgressStore(redisClient *redis.Client) *GraphRebuildProgressStore {
	return &GraphRebuildProgressStore{redis: redisClient}
}

func graphRebuildKey(tenantID uint64, kbID string) string {
	return fmt.Sprintf("graph:rebuild:%d:%s", tenantID, kbID)
}

func (s *GraphRebuildProgressStore) available() bool {
	return s != nil && s.redis != nil
}

// Start records a new rebuild job. total is expected extract-task count (0 allowed).
func (s *GraphRebuildProgressStore) Start(ctx context.Context, tenantID uint64, kbID string, total, documentCount int, requireReview bool) error {
	if !s.available() {
		return nil
	}
	now := time.Now().UTC()
	p := types.GraphRebuildProgress{
		Status:        types.GraphRebuildRunning,
		StartedAt:     now,
		Total:         total,
		Processed:     0,
		Percent:       0,
		RequireReview: requireReview,
		DocumentCount: documentCount,
	}
	if total <= 0 {
		p.Status = types.GraphRebuildCompleted
		p.Percent = 100
		p.FinishedAt = &now
		if requireReview {
			p.Status = types.GraphRebuildAwaitingReview
			p.Message = "no_extract_tasks"
		}
	}
	return s.save(ctx, tenantID, kbID, &p)
}

// Get returns current progress or idle.
func (s *GraphRebuildProgressStore) Get(ctx context.Context, tenantID uint64, kbID string) (*types.GraphRebuildProgress, error) {
	if !s.available() {
		return &types.GraphRebuildProgress{Status: types.GraphRebuildIdle}, nil
	}
	raw, err := s.redis.Get(ctx, graphRebuildKey(tenantID, kbID)).Bytes()
	if err == redis.Nil {
		return &types.GraphRebuildProgress{Status: types.GraphRebuildIdle}, nil
	}
	if err != nil {
		return nil, err
	}
	var p types.GraphRebuildProgress
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	p.Percent = calcPercent(p.Processed, p.Total, p.Status)
	return &p, nil
}

// IncrProcessed increments extract completion; finalizes when processed >= total.
func (s *GraphRebuildProgressStore) IncrProcessed(ctx context.Context, tenantID uint64, kbID string) (*types.GraphRebuildProgress, error) {
	if !s.available() {
		return nil, nil
	}
	p, err := s.Get(ctx, tenantID, kbID)
	if err != nil || p == nil || p.Status != types.GraphRebuildRunning {
		return p, err
	}
	p.Processed++
	if p.Processed > p.Total && p.Total > 0 {
		p.Processed = p.Total
	}
	p.Percent = calcPercent(p.Processed, p.Total, p.Status)
	if p.Total > 0 && p.Processed >= p.Total {
		now := time.Now().UTC()
		p.FinishedAt = &now
		if p.RequireReview {
			p.Status = types.GraphRebuildAwaitingReview
			p.Percent = 100
		} else {
			p.Status = types.GraphRebuildCompleted
			p.Percent = 100
		}
	}
	if err := s.save(ctx, tenantID, kbID, p); err != nil {
		return p, err
	}
	return p, nil
}

// RefreshFinalState updates awaiting_review pending count / completed when review cleared.
func (s *GraphRebuildProgressStore) RefreshFinalState(ctx context.Context, tenantID uint64, kbID string, pendingReview int64) (*types.GraphRebuildProgress, error) {
	p, err := s.Get(ctx, tenantID, kbID)
	if err != nil || p == nil {
		return p, err
	}
	if p.Status != types.GraphRebuildAwaitingReview && p.Status != types.GraphRebuildCompleted && p.Status != types.GraphRebuildRunning {
		return p, nil
	}
	p.PendingReview = pendingReview
	if p.Status == types.GraphRebuildAwaitingReview {
		if !p.RequireReview || pendingReview == 0 {
			now := time.Now().UTC()
			if p.FinishedAt == nil {
				p.FinishedAt = &now
			}
			p.Status = types.GraphRebuildCompleted
			p.Percent = 100
			_ = s.save(ctx, tenantID, kbID, p)
		} else {
			_ = s.save(ctx, tenantID, kbID, p)
		}
	}
	// Stuck running with total 0 already handled in Start; if running but long past with processed==total race:
	if p.Status == types.GraphRebuildRunning && p.Total > 0 && p.Processed >= p.Total {
		now := time.Now().UTC()
		p.FinishedAt = &now
		p.Percent = 100
		if p.RequireReview && pendingReview > 0 {
			p.Status = types.GraphRebuildAwaitingReview
		} else if p.RequireReview {
			p.Status = types.GraphRebuildAwaitingReview
		} else {
			p.Status = types.GraphRebuildCompleted
		}
		_ = s.save(ctx, tenantID, kbID, p)
	}
	p.Percent = calcPercent(p.Processed, p.Total, p.Status)
	return p, nil
}

func (s *GraphRebuildProgressStore) save(ctx context.Context, tenantID uint64, kbID string, p *types.GraphRebuildProgress) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return s.redis.Set(ctx, graphRebuildKey(tenantID, kbID), b, graphRebuildTTL).Err()
}

func calcPercent(processed, total int, status string) int {
	if status == types.GraphRebuildCompleted || status == types.GraphRebuildAwaitingReview {
		if total <= 0 || processed >= total {
			return 100
		}
	}
	if total <= 0 {
		if status == types.GraphRebuildRunning {
			return 5
		}
		return 0
	}
	pct := processed * 100 / total
	if pct > 100 {
		pct = 100
	}
	if pct < 0 {
		pct = 0
	}
	return pct
}
