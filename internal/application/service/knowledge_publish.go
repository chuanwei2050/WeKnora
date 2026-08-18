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
)

type KnowledgePublishService struct {
	repo interfaces.KnowledgeGovernanceRepository
}

func NewKnowledgePublishService(repo interfaces.KnowledgeGovernanceRepository) interfaces.TaskHandler {
	return &KnowledgePublishService{repo: repo}
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
	if version.Status == types.KnowledgeVersionActive || version.Status == types.KnowledgeVersionScheduled {
		return nil
	}
	if version.Status != types.KnowledgeVersionIndexing && version.Status != types.KnowledgeVersionPublishFailed {
		return fmt.Errorf("knowledge version cannot be published from %s", version.Status)
	}
	if err := s.repo.ActivateVersion(ctx, payload.TenantID, payload.VersionID, time.Now().UTC()); err != nil {
		_ = s.repo.UpdateVersionStatus(ctx, payload.TenantID, payload.VersionID, types.KnowledgeVersionPublishFailed)
		return fmt.Errorf("activate governed knowledge version: %w", err)
	}
	return s.repo.CreateReview(ctx, &types.KnowledgeVersionReview{
		ID: uuid.NewString(), VersionID: payload.VersionID, ReviewerID: "system", Action: "publish", CreatedAt: time.Now().UTC(),
	})
}
