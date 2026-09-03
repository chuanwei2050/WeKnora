package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
)

type knowledgeDirectoryService struct {
	repo interfaces.KnowledgeDirectoryRepository
	task interfaces.TaskEnqueuer
}

func NewKnowledgeDirectoryService(repo interfaces.KnowledgeDirectoryRepository, task interfaces.TaskEnqueuer) interfaces.KnowledgeDirectoryService {
	return &knowledgeDirectoryService{repo: repo, task: task}
}

func (s *knowledgeDirectoryService) Create(ctx context.Context, tenantID uint64, kbID, tagID string, parentID *string, name string) (*types.KnowledgeDirectory, error) {
	displayName, normalizedName, err := types.NormalizeDirectoryName(name)
	if err != nil {
		return nil, err
	}
	if parentID != nil {
		if err = s.validateActivePath(ctx, tenantID, kbID, tagID, *parentID); err != nil {
			return nil, err
		}
	}
	directory := &types.KnowledgeDirectory{TenantID: tenantID, KnowledgeBaseID: kbID, TagID: tagID, ParentID: parentID, Name: displayName, NormalizedName: normalizedName}
	return directory, s.repo.Create(ctx, directory)
}

func (s *knowledgeDirectoryService) List(ctx context.Context, tenantID uint64, kbID string, parentID *string, page *types.Pagination, sortBy, sortOrder string, visibility types.KnowledgeVisibilityFilter, tagIDs ...string) ([]*types.KnowledgeDirectory, int64, error) {
	tagID := firstTagID(tagIDs)
	if parentID != nil {
		if err := s.validateActivePath(ctx, tenantID, kbID, tagID, *parentID); err != nil {
			return nil, 0, err
		}
	}
	if page == nil {
		page = &types.Pagination{}
	}
	return s.repo.ListChildren(ctx, tenantID, kbID, parentID, page.Offset(), page.Limit(), sortBy, sortOrder, visibility, tagID)
}

func (s *knowledgeDirectoryService) Rename(ctx context.Context, tenantID uint64, kbID, tagID, id, name string) error {
	displayName, normalizedName, err := types.NormalizeDirectoryName(name)
	if err != nil {
		return err
	}
	if err := s.validateActivePath(ctx, tenantID, kbID, tagID, id); err != nil {
		return err
	}
	return s.repo.Rename(ctx, tenantID, kbID, id, displayName, normalizedName, tagID)
}

func (s *knowledgeDirectoryService) Move(ctx context.Context, tenantID uint64, kbID, tagID, id string, parentID *string) error {
	return s.repo.Move(ctx, tenantID, kbID, id, parentID, tagID)
}

func (s *knowledgeDirectoryService) MoveEntries(ctx context.Context, tenantID uint64, kbID, tagID string, directoryIDs, knowledgeIDs []string, parentID *string) error {
	return s.repo.MoveEntries(ctx, tenantID, kbID, directoryIDs, knowledgeIDs, parentID, tagID)
}

func (s *knowledgeDirectoryService) DeleteEmpty(ctx context.Context, tenantID uint64, kbID, tagID, id string) error {
	return s.repo.DeleteEmpty(ctx, tenantID, kbID, id, tagID)
}

func (s *knowledgeDirectoryService) Breadcrumb(ctx context.Context, tenantID uint64, kbID, tagID, id string) ([]types.PathNode, error) {
	return s.repo.Breadcrumb(ctx, tenantID, kbID, id, tagID)
}

func (s *knowledgeDirectoryService) EnsureUploadPath(ctx context.Context, tenantID uint64, kbID string, parentID *string, relativePath string, tagIDs ...string) (*types.KnowledgeDirectory, error) {
	tagID := firstTagID(tagIDs)
	segments, err := types.ParseDirectoryPath(relativePath)
	if err != nil {
		return nil, err
	}
	if len(segments) == 0 {
		if parentID == nil {
			return nil, nil
		}
		if err := s.validateActivePath(ctx, tenantID, kbID, tagID, *parentID); err != nil {
			return nil, err
		}
		return s.repo.Get(ctx, tenantID, kbID, *parentID, tagID)
	}
	if parentID != nil {
		if err := s.validateActivePath(ctx, tenantID, kbID, tagID, *parentID); err != nil {
			return nil, err
		}
	}
	return s.repo.EnsurePath(ctx, tenantID, kbID, parentID, segments, tagID)
}

func firstTagID(tagIDs []string) string {
	if len(tagIDs) == 0 {
		return ""
	}
	return strings.TrimSpace(tagIDs[0])
}

func (s *knowledgeDirectoryService) validateActivePath(ctx context.Context, tenantID uint64, kbID, tagID, id string) error {
	current := id
	for depth := 0; current != ""; depth++ {
		if depth >= types.MaxDirectoryDepth {
			return types.ErrInvalidDirectoryPath
		}
		directory, err := s.repo.Get(ctx, tenantID, kbID, current, tagID)
		if err != nil {
			return err
		}
		if directory.Status != types.DirectoryStatusActive {
			return fmt.Errorf("document directory is deleting")
		}
		if directory.ParentID == nil {
			return nil
		}
		current = *directory.ParentID
	}
	return nil
}

func (s *knowledgeDirectoryService) ListSubtree(ctx context.Context, tenantID uint64, kbID, tagID, rootID string) ([]*types.KnowledgeDirectory, []*types.Knowledge, error) {
	return s.repo.ListSubtree(ctx, tenantID, kbID, rootID, tagID)
}

func deletionSnapshotDigest(directories []*types.KnowledgeDirectory, knowledges []*types.Knowledge) string {
	parts := make([]string, 0, len(directories)+len(knowledges))
	for _, directory := range directories {
		parts = append(parts, "d:"+directory.ID+":"+directory.UpdatedAt.UTC().Format(time.RFC3339Nano))
	}
	for _, knowledge := range knowledges {
		parts = append(parts, "k:"+knowledge.ID+":"+knowledge.UpdatedAt.UTC().Format(time.RFC3339Nano))
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

func (s *knowledgeDirectoryService) PreviewDelete(ctx context.Context, tenantID uint64, kbID, tagID, rootID, requestedBy string) (*types.KnowledgeDirectoryDeletePreview, error) {
	directories, knowledges, err := s.repo.ListSubtree(ctx, tenantID, kbID, rootID, tagID)
	if err != nil {
		return nil, err
	}
	for _, directory := range directories {
		if directory.ID == rootID && directory.Status != types.DirectoryStatusActive {
			return nil, fmt.Errorf("document directory is deleting")
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	expiresAt := time.Now().Add(10 * time.Minute)
	if err := s.repo.CreateDeleteToken(ctx, &types.KnowledgeDirectoryDeleteToken{TokenHash: hex.EncodeToString(hash[:]), TenantID: tenantID, KnowledgeBaseID: kbID, RootDirectoryID: rootID, RequestedBy: requestedBy, SnapshotDigest: deletionSnapshotDigest(directories, knowledges), ExpiresAt: expiresAt}); err != nil {
		return nil, err
	}
	preview := &types.KnowledgeDirectoryDeletePreview{DirectoryCount: len(directories), DocumentCount: len(knowledges), ConfirmationToken: token, ExpiresAt: expiresAt}
	for _, knowledge := range knowledges {
		preview.TotalStorageSize += knowledge.StorageSize
	}
	return preview, nil
}

func (s *knowledgeDirectoryService) ConfirmDelete(ctx context.Context, tenantID uint64, kbID, tagID, rootID, requestedBy, confirmationToken string) (*types.KnowledgeDirectoryDeleteTask, error) {
	if _, err := s.repo.Get(ctx, tenantID, kbID, rootID, tagID); err != nil {
		return nil, err
	}
	hash := sha256.Sum256([]byte(confirmationToken))
	task, batches, err := s.repo.ConfirmDelete(ctx, tenantID, kbID, rootID, requestedBy, hex.EncodeToString(hash[:]), time.Now())
	if err != nil {
		return nil, err
	}
	if err := s.dispatchDeleteBatches(ctx, tenantID, kbID, task.ID, batches); err != nil {
		return task, err
	}
	return task, nil
}

func (s *knowledgeDirectoryService) dispatchDeleteBatches(ctx context.Context, tenantID uint64, kbID, taskID string, batches []*types.KnowledgeDirectoryDeleteBatch) error {
	for _, batch := range batches {
		if batch.Status != types.DirectoryDeleteStatusPending {
			continue
		}
		payload := types.KnowledgeListDeletePayload{TenantID: tenantID, KnowledgeBaseID: kbID, KnowledgeIDs: []string(batch.KnowledgeIDs), DirectoryDeleteTaskID: taskID, DirectoryDeleteBatchID: batch.ID}
		payloadBytes, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return marshalErr
		}
		queueTask := asynq.NewTask(types.TypeKnowledgeListDelete, payloadBytes, asynq.Queue("low"), asynq.MaxRetry(3), asynq.TaskID(batch.AsynqTaskID))
		if _, enqueueErr := s.task.Enqueue(queueTask); enqueueErr != nil && !errors.Is(enqueueErr, asynq.ErrTaskIDConflict) {
			return fmt.Errorf("delete task accepted but batch dispatch is pending: %w", enqueueErr)
		}
		if err := s.repo.MarkDeleteBatchDispatched(ctx, taskID, batch.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *knowledgeDirectoryService) DispatchPendingDeletes(ctx context.Context) error {
	tasks, batchesByTask, err := s.repo.ListPendingDeleteBatches(ctx, 100)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if err := s.dispatchDeleteBatches(ctx, task.TenantID, task.KnowledgeBaseID, task.ID, batchesByTask[task.ID]); err != nil {
			return err
		}
	}
	return nil
}

func (s *knowledgeDirectoryService) RetryDeleteTask(ctx context.Context, tenantID uint64, kbID, taskID string) error {
	if err := s.repo.RetryFailedDeleteBatches(ctx, tenantID, kbID, taskID); err != nil {
		return err
	}
	return s.DispatchPendingDeletes(ctx)
}

func (s *knowledgeDirectoryService) GetDeleteTask(ctx context.Context, tenantID uint64, kbID, taskID string) (*types.KnowledgeDirectoryDeleteTask, []*types.KnowledgeDirectoryDeleteBatch, error) {
	_, batches, err := s.repo.GetDeleteTask(ctx, tenantID, kbID, taskID)
	if err != nil {
		return nil, nil, err
	}
	_ = s.dispatchDeleteBatches(ctx, tenantID, kbID, taskID, batches)
	return s.repo.GetDeleteTask(ctx, tenantID, kbID, taskID)
}
