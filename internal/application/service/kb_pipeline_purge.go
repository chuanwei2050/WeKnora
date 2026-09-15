package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// rebuildPipelineTaskTypes are asynq types owned by document-pipeline rebuild/reparse.
// Stop must only touch these — never flush unrelated queues (chat, wiki, sync, …).
var rebuildPipelineTaskTypes = map[string]struct{}{
	types.TypeDocumentProcess:      {},
	types.TypeImageMultimodal:      {},
	types.TypeKnowledgePostProcess: {},
}

var rebuildPipelineQueues = []string{
	"default",
	types.LargeDocumentQueue,
	"low",
	"critical",
}

func isRebuildPipelineTaskType(taskType string) bool {
	_, ok := rebuildPipelineTaskTypes[taskType]
	return ok
}

func payloadKnowledgeID(payload []byte) string {
	var peek struct {
		KnowledgeID string `json:"knowledge_id"`
	}
	if err := json.Unmarshal(payload, &peek); err != nil {
		return ""
	}
	return peek.KnowledgeID
}

func knowledgeIDSet(ids []string) map[string]struct{} {
	out := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		out[id] = struct{}{}
	}
	return out
}

// PurgeDocumentPipelineTasks removes rebuild-related asynq tasks and multimodal
// pending counters for the given knowledge IDs. Active in-flight workers are not
// force-killed; they must skip via deliberate interrupt markers.
// Returns how many asynq tasks were deleted.
func (s *knowledgeService) PurgeDocumentPipelineTasks(ctx context.Context, knowledgeIDs []string) (int, error) {
	targets := knowledgeIDSet(knowledgeIDs)
	if len(targets) == 0 {
		return 0, nil
	}

	clearedKeys := s.clearMultimodalPendingKeys(ctx, knowledgeIDs)
	deleted := s.deleteRebuildAsynqTasks(ctx, targets)
	if clearedKeys > 0 || deleted > 0 {
		logger.Infof(ctx, "purged rebuild pipeline leftovers: asynq_tasks=%d multimodal_keys=%d targets=%d",
			deleted, clearedKeys, len(targets))
	}
	return deleted, nil
}

func (s *knowledgeService) clearMultimodalPendingKeys(ctx context.Context, knowledgeIDs []string) int {
	if s.redisClient == nil {
		return 0
	}
	cleared := 0
	for _, id := range knowledgeIDs {
		if id == "" {
			continue
		}
		keys := []string{multimodalPendingKey(id, "")}
		pattern := fmt.Sprintf("multimodal:pending:%s:*", id)
		iter := s.redisClient.Scan(ctx, 0, pattern, 100).Iterator()
		for iter.Next(ctx) {
			keys = append(keys, iter.Val())
		}
		if err := iter.Err(); err != nil {
			logger.Warnf(ctx, "scan multimodal pending keys for %s failed: %v", id, err)
		}
		if len(keys) == 0 {
			continue
		}
		n, err := s.redisClient.Del(ctx, keys...).Result()
		if err != nil && err != redis.Nil {
			logger.Warnf(ctx, "delete multimodal pending keys for %s failed: %v", id, err)
			continue
		}
		cleared += int(n)
	}
	return cleared
}

func (s *knowledgeService) newAsynqInspector() *asynq.Inspector {
	if s.redisClient == nil {
		return nil
	}
	opt := s.redisClient.Options()
	if opt == nil {
		return nil
	}
	return asynq.NewInspector(asynq.RedisClientOpt{
		Addr:         opt.Addr,
		Username:     opt.Username,
		Password:     opt.Password,
		DB:           opt.DB,
		DialTimeout:  opt.DialTimeout,
		ReadTimeout:  opt.ReadTimeout,
		WriteTimeout: opt.WriteTimeout,
		PoolSize:     opt.PoolSize,
	})
}

func (s *knowledgeService) deleteRebuildAsynqTasks(ctx context.Context, targets map[string]struct{}) int {
	inspector := s.newAsynqInspector()
	if inspector == nil {
		return 0
	}
	defer inspector.Close()

	deleted := 0
	for _, queue := range rebuildPipelineQueues {
		deleted += deleteMatchingTasksInQueue(ctx, inspector, queue, targets)
	}
	return deleted
}

func deleteMatchingTasksInQueue(ctx context.Context, inspector *asynq.Inspector, queue string, targets map[string]struct{}) int {
	deleted := 0
	listers := []func(string, ...asynq.ListOption) ([]*asynq.TaskInfo, error){
		inspector.ListPendingTasks,
		inspector.ListRetryTasks,
		inspector.ListScheduledTasks,
		inspector.ListArchivedTasks,
	}
	for _, list := range listers {
		for {
			pageDeleted := 0
			page := 1
			for {
				tasks, err := list(queue, asynq.PageSize(100), asynq.Page(page))
				if err != nil {
					// Queue may not exist yet — ignore.
					break
				}
				if len(tasks) == 0 {
					break
				}
				removedOnPage := 0
				for _, task := range tasks {
					if task == nil || !isRebuildPipelineTaskType(task.Type) {
						continue
					}
					kid := payloadKnowledgeID(task.Payload)
					if _, ok := targets[kid]; !ok {
						continue
					}
					if err := inspector.DeleteTask(queue, task.ID); err != nil {
						logger.Warnf(ctx, "delete rebuild asynq task queue=%s id=%s type=%s: %v", queue, task.ID, task.Type, err)
						continue
					}
					deleted++
					pageDeleted++
					removedOnPage++
				}
				if len(tasks) < 100 {
					break
				}
				// Deleting shifts later pages forward; only advance when this page
				// had no matching deletes, otherwise rescan the same page index.
				if removedOnPage == 0 {
					page++
				}
			}
			// Full pass with no deletes means this list is clean.
			if pageDeleted == 0 {
				break
			}
		}
	}
	return deleted
}
