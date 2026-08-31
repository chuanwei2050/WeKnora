package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

type KnowledgePublishService struct {
	repo          interfaces.KnowledgeGovernanceRepository
	knowledgeRepo interfaces.KnowledgeRepository
	kbService     interfaces.KnowledgeBaseService
	task          interfaces.TaskEnqueuer
	redisClient   *redis.Client
}

func NewKnowledgePublishService(
	repo interfaces.KnowledgeGovernanceRepository,
	knowledgeRepo interfaces.KnowledgeRepository,
	kbService interfaces.KnowledgeBaseService,
	task interfaces.TaskEnqueuer,
	redisClient *redis.Client,
) interfaces.TaskHandler {
	return &KnowledgePublishService{repo: repo, knowledgeRepo: knowledgeRepo, kbService: kbService, task: task, redisClient: redisClient}
}

func (s *KnowledgePublishService) Handle(ctx context.Context, task *asynq.Task) error {
	var payload types.KnowledgePublishPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal knowledge publish payload: %w", err)
	}
	ctx = context.WithValue(ctx, types.TenantIDContextKey, payload.TenantID)
	version, err := s.repo.GetVersion(ctx, payload.TenantID, payload.VersionID)
	if err != nil {
		return err
	}
	if version == nil || version.KnowledgeID != payload.KnowledgeID {
		return fmt.Errorf("knowledge version not found")
	}
	if version.Status == types.KnowledgeVersionActive {
		if err := s.ensurePublishReview(ctx, payload.VersionID); err != nil {
			return err
		}
		return s.enqueueWikiAfterActivation(ctx, payload)
	}
	if version.Status == types.KnowledgeVersionScheduled {
		return nil
	}
	if version.Status != types.KnowledgeVersionIndexing && version.Status != types.KnowledgeVersionPublishFailed {
		return fmt.Errorf("knowledge version cannot be published from %s", version.Status)
	}
	if s.knowledgeRepo != nil {
		knowledge, err := s.knowledgeRepo.GetKnowledgeByID(ctx, payload.TenantID, payload.KnowledgeID)
		if err != nil {
			return fmt.Errorf("load knowledge before activation: %w", err)
		}
		if knowledge == nil || knowledge.PendingVersionID != payload.VersionID {
			// A newer contribution has replaced this queued publication. The
			// repository repeats this check inside its activation transaction.
			return nil
		}
	}
	if err := s.repo.ActivateVersion(ctx, payload.TenantID, payload.VersionID, time.Now().UTC()); err != nil {
		_ = s.repo.UpdateVersionStatus(ctx, payload.TenantID, payload.VersionID, types.KnowledgeVersionPublishFailed)
		return fmt.Errorf("activate governed knowledge version: %w", err)
	}
	if err := s.ensurePublishReview(ctx, payload.VersionID); err != nil {
		return err
	}
	if err := s.enqueueWikiAfterActivation(ctx, payload); err != nil {
		return err
	}
	return nil
}

func (s *KnowledgePublishService) enqueueWikiAfterActivation(ctx context.Context, payload types.KnowledgePublishPayload) error {
	if s.knowledgeRepo == nil || s.task == nil {
		return nil
	}
	knowledge, getErr := s.knowledgeRepo.GetKnowledgeByID(ctx, payload.TenantID, payload.KnowledgeID)
	if getErr != nil {
		return fmt.Errorf("load knowledge after activation: %w", getErr)
	}
	if knowledge == nil || s.kbService == nil || knowledge.CurrentVersionID != payload.VersionID {
		return nil
	}
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, knowledge.KnowledgeBaseID)
	if err != nil {
		return fmt.Errorf("load knowledge base after activation: %w", err)
	}
	if kb == nil || !kb.IsWikiEnabled() {
		return nil
	}
	// Wiki content is generated only after activation so pending content never
	// becomes visible through the production wiki. The active retry path also
	// calls this method, so a transient enqueue failure is recoverable.
	if err := EnqueueWikiIngest(ctx, s.task, s.redisClient, payload.TenantID, knowledge.KnowledgeBaseID, knowledge.ID, payload.VersionID); err != nil {
		return fmt.Errorf("enqueue wiki ingest after activation: %w", err)
	}
	return nil
}

func (s *KnowledgePublishService) ensurePublishReview(ctx context.Context, versionID string) error {
	reviews, err := s.repo.ListReviews(ctx, versionID)
	if err != nil {
		return fmt.Errorf("load publish review: %w", err)
	}
	for _, review := range reviews {
		if review != nil && review.Action == "publish" {
			return nil
		}
	}
	return s.repo.CreateReview(ctx, &types.KnowledgeVersionReview{
		ID: uuid.NewString(), VersionID: versionID, ReviewerID: "system", Action: "publish", CreatedAt: time.Now().UTC(),
	})
}
