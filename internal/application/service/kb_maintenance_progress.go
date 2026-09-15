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
if decoded.status == "running" or decoded.status == "canceling" then
  return {0, current}
end
redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[2])
return {1, ARGV[1]}
`)

type KBMaintenanceStore struct{ redis *redis.Client }

func NewKBMaintenanceStore(client *redis.Client) *KBMaintenanceStore {
	return &KBMaintenanceStore{redis: client}
}

func kbMaintenanceKey(tenantID uint64, kbID string) string {
	return fmt.Sprintf("kb:maintenance:%d:%s", tenantID, kbID)
}

func kbMaintenanceBusy(status string) bool {
	return status == "running" || status == "canceling"
}

// applyKBMaintenanceCancel marks a running job as canceled and releases the
// maintenance lock immediately. Document-pipeline callers should interrupt and
// purge target leftovers after Cancel so staging sees IsRunning=false first.
func applyKBMaintenanceCancel(p *types.KBMaintenanceProgress, now time.Time) {
	p.Message = "canceled_by_user"
	p.HeartbeatAt = now
	p.Status = "canceled"
	p.FinishedAt = &now
}

// applyKBMaintenanceUpdate mutates progress in place.
// Returns false when the record must stay untouched (terminal canceled or run mismatch handled by caller).
func applyKBMaintenanceUpdate(p *types.KBMaintenanceProgress, processed, failed int, message string, finished bool, now time.Time) bool {
	if p.Status == "canceled" {
		return false
	}
	p.Processed, p.Failed, p.Message, p.HeartbeatAt = processed, failed, message, now
	if p.Total > 0 {
		p.Percent = processed * 100 / p.Total
	}
	if p.Percent > 100 {
		p.Percent = 100
	}
	if !finished {
		return true
	}
	p.FinishedAt = &now
	p.Percent = 100
	if p.Status == "canceling" {
		p.Status = "canceled"
		return true
	}
	if failed > 0 {
		p.Status = "completed_with_failures"
	} else {
		p.Status = "completed"
	}
	return true
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
		current.EnqueueDone = true
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

func (s *KBMaintenanceStore) Cancel(ctx context.Context, tenantID uint64, kbID, runID string) (*types.KBMaintenanceProgress, error) {
	key := kbMaintenanceKey(tenantID, kbID)
	var out *types.KBMaintenanceProgress
	err := s.redis.Watch(ctx, func(tx *redis.Tx) error {
		raw, err := tx.Get(ctx, key).Bytes()
		if err != nil {
			return err
		}
		var p types.KBMaintenanceProgress
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if p.RunID != runID || (p.Status != "running" && p.Status != "canceling") {
			out = &p
			return nil
		}
		applyKBMaintenanceCancel(&p, time.Now().UTC())
		b, err := json.Marshal(&p)
		if err != nil {
			return err
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error { pipe.Set(ctx, key, b, kbMaintenanceTTL); return nil })
		if err == nil {
			out = &p
		}
		return err
	}, key)
	return out, err
}

func (s *KBMaintenanceStore) IsRunning(ctx context.Context, tenantID uint64, kbID, runID string) bool {
	p, err := s.Get(ctx, tenantID, kbID)
	return err == nil && p != nil && p.RunID == runID && p.Status == "running"
}

// MarkEnqueueDone flips enqueue_done after document-pipeline staging finishes.
// No-op when the run id no longer matches or the job already left running/canceling.
func (s *KBMaintenanceStore) MarkEnqueueDone(ctx context.Context, tenantID uint64, kbID, runID string) error {
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
		if p.RunID != runID || !kbMaintenanceBusy(p.Status) {
			return nil
		}
		if p.EnqueueDone {
			return nil
		}
		p.EnqueueDone = true
		p.HeartbeatAt = time.Now().UTC()
		b, err := json.Marshal(&p)
		if err != nil {
			return err
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error { pipe.Set(ctx, key, b, kbMaintenanceTTL); return nil })
		return err
	}, key)
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
		if !applyKBMaintenanceUpdate(&p, processed, failed, message, finished, time.Now().UTC()) {
			return nil
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
