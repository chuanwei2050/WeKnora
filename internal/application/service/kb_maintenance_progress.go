package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/redis/go-redis/v9"
)

const kbMaintenanceTTL = 24 * time.Hour

var tryStartKBMaintenanceScript = redis.NewScript(`
local current = redis.call("GET", KEYS[1])
if not current then
  redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[2])
  return {1, ARGV[1]}
end
local decoded = cjson.decode(current)
if decoded.status ~= "running" then
  redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[2])
  return {1, ARGV[1]}
end
return {0, current}
`)

type KBMaintenanceStore struct{ redis *redis.Client }

func NewKBMaintenanceStore(client *redis.Client) *KBMaintenanceStore {
	return &KBMaintenanceStore{redis: client}
}

func kbMaintenanceKey(tenantID uint64, kbID string) string {
	return fmt.Sprintf("kb:maintenance:%d:%s", tenantID, kbID)
}

func (s *KBMaintenanceStore) TryStart(ctx context.Context, tenantID uint64, kbID string, operation types.KBMaintenanceOperation, total int, targetKnowledgeIDs []string) (*types.KBMaintenanceProgress, bool, error) {
	if s == nil || s.redis == nil {
		return nil, false, fmt.Errorf("maintenance locking requires redis")
	}
	now := time.Now().UTC()
	p := &types.KBMaintenanceProgress{RunID: fmt.Sprintf("%d", now.UnixNano()), Operation: operation, Status: "running", Total: total, StartedAt: now, HeartbeatAt: now, TargetKnowledgeIDs: targetKnowledgeIDs}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, false, err
	}
	result, err := tryStartKBMaintenanceScript.Run(ctx, s.redis, []string{kbMaintenanceKey(tenantID, kbID)}, b, kbMaintenanceTTL.Milliseconds()).Slice()
	if err != nil {
		return nil, false, err
	}
	if len(result) != 2 {
		return nil, false, fmt.Errorf("unexpected maintenance lock result")
	}
	acquired, ok := result[0].(int64)
	if !ok {
		return nil, false, fmt.Errorf("unexpected maintenance lock status")
	}
	raw, ok := result[1].(string)
	if !ok {
		return nil, false, fmt.Errorf("unexpected maintenance lock payload")
	}
	var current types.KBMaintenanceProgress
	if err := json.Unmarshal([]byte(raw), &current); err != nil {
		return nil, false, err
	}
	return &current, acquired == 1, nil
}

// AdvancePhase atomically moves a running maintenance job into its next phase.
// Only one observer can perform the transition, which prevents duplicate tasks.
func (s *KBMaintenanceStore) AdvancePhase(ctx context.Context, tenantID uint64, kbID, runID string, from, to types.KBMaintenanceOperation, total int, targetKnowledgeIDs []string) (*types.KBMaintenanceProgress, bool, error) {
	key := kbMaintenanceKey(tenantID, kbID)
	var next *types.KBMaintenanceProgress
	err := s.redis.Watch(ctx, func(tx *redis.Tx) error {
		raw, err := tx.Get(ctx, key).Bytes()
		if err != nil {
			return err
		}
		var current types.KBMaintenanceProgress
		if err := json.Unmarshal(raw, &current); err != nil {
			return err
		}
		if current.RunID != runID || current.Operation != from || current.Status != "running" {
			return nil
		}
		now := time.Now().UTC()
		current.Operation = to
		current.Total = total
		current.Processed = 0
		current.Failed = 0
		current.Percent = 0
		current.Message = ""
		current.FinishedAt = nil
		current.HeartbeatAt = now
		current.TargetKnowledgeIDs = targetKnowledgeIDs
		b, err := json.Marshal(&current)
		if err != nil {
			return err
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.Set(ctx, key, b, kbMaintenanceTTL)
			return nil
		})
		if err == nil {
			next = &current
		}
		return err
	}, key)
	return next, next != nil, err
}

func (s *KBMaintenanceStore) Get(ctx context.Context, tenantID uint64, kbID string) (*types.KBMaintenanceProgress, error) {
	if s == nil || s.redis == nil {
		return nil, nil
	}
	raw, err := s.redis.Get(ctx, kbMaintenanceKey(tenantID, kbID)).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p types.KBMaintenanceProgress
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *KBMaintenanceStore) Update(ctx context.Context, tenantID uint64, kbID, runID string, processed, failed int, message string, finished bool) error {
	key := kbMaintenanceKey(tenantID, kbID)
	return s.redis.Watch(ctx, func(tx *redis.Tx) error {
		raw, err := tx.Get(ctx, key).Bytes()
		if err != nil {
			return err
		}
		var p types.KBMaintenanceProgress
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if p.RunID != runID {
			return nil
		}
		p.Processed, p.Failed, p.Message, p.HeartbeatAt = processed, failed, message, time.Now().UTC()
		if p.Total > 0 {
			p.Percent = processed * 100 / p.Total
		}
		if p.Percent > 100 {
			p.Percent = 100
		}
		if finished {
			now := time.Now().UTC()
			p.FinishedAt = &now
			p.Percent = 100
			if failed > 0 {
				p.Status = "completed_with_failures"
			} else {
				p.Status = "completed"
			}
		}
		b, err := json.Marshal(&p)
		if err != nil {
			return err
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error { pipe.Set(ctx, key, b, kbMaintenanceTTL); return nil })
		return err
	}, key)
}

func (s *KBMaintenanceStore) Fail(ctx context.Context, tenantID uint64, kbID, runID, message string) error {
	p, err := s.Get(ctx, tenantID, kbID)
	if err != nil || p == nil || p.RunID != runID {
		return err
	}
	p.Failed++
	return s.Update(ctx, tenantID, kbID, runID, p.Processed, p.Failed, message, true)
}
