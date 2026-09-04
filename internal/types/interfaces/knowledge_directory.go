package interfaces

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

type KnowledgeDirectoryRepository interface {
	Create(ctx context.Context, directory *types.KnowledgeDirectory) error
	Get(ctx context.Context, tenantID uint64, kbID, id string, tagIDs ...string) (*types.KnowledgeDirectory, error)
	FindChild(ctx context.Context, tenantID uint64, kbID string, parentID *string, normalizedName string, tagIDs ...string) (*types.KnowledgeDirectory, error)
	ListChildren(ctx context.Context, tenantID uint64, kbID string, parentID *string, offset, limit int, sortBy, sortOrder string, visibility types.KnowledgeVisibilityFilter, tagIDs ...string) ([]*types.KnowledgeDirectory, int64, error)
	Rename(ctx context.Context, tenantID uint64, kbID, id, name, normalizedName string, tagIDs ...string) error
	Move(ctx context.Context, tenantID uint64, kbID, id string, parentID *string, tagIDs ...string) error
	MoveEntries(ctx context.Context, tenantID uint64, kbID string, directoryIDs, knowledgeIDs []string, parentID *string, tagIDs ...string) error
	MoveSubtreesToTag(ctx context.Context, tenantID uint64, kbID, sourceTagID, targetTagID string, directoryIDs, knowledgeIDs []string) ([]string, error)
	DeleteEmpty(ctx context.Context, tenantID uint64, kbID, id string, tagIDs ...string) error
	DeleteByTag(ctx context.Context, tenantID uint64, kbID, tagID string) error
	Breadcrumb(ctx context.Context, tenantID uint64, kbID, id string, tagIDs ...string) ([]types.PathNode, error)
	EnsurePath(ctx context.Context, tenantID uint64, kbID string, parentID *string, segments []string, tagIDs ...string) (*types.KnowledgeDirectory, error)
	ListSubtree(ctx context.Context, tenantID uint64, kbID, rootID string, tagIDs ...string) ([]*types.KnowledgeDirectory, []*types.Knowledge, error)
	CreateDeleteToken(ctx context.Context, token *types.KnowledgeDirectoryDeleteToken) error
	ConfirmDelete(ctx context.Context, tenantID uint64, kbID, rootID, requestedBy, tokenHash string, now time.Time) (*types.KnowledgeDirectoryDeleteTask, []*types.KnowledgeDirectoryDeleteBatch, error)
	GetDeleteTask(ctx context.Context, tenantID uint64, kbID, taskID string) (*types.KnowledgeDirectoryDeleteTask, []*types.KnowledgeDirectoryDeleteBatch, error)
	ValidateDeleteBatch(ctx context.Context, payload *types.KnowledgeListDeletePayload) error
	IsDeleteBatchClean(ctx context.Context, payload *types.KnowledgeListDeletePayload) (bool, error)
	CompleteDeleteBatch(ctx context.Context, taskID, batchID string, executionErr error) error
	MarkDeleteBatchDispatched(ctx context.Context, taskID, batchID string) error
	ListPendingDeleteBatches(ctx context.Context, limit int) ([]*types.KnowledgeDirectoryDeleteTask, map[string][]*types.KnowledgeDirectoryDeleteBatch, error)
	RetryFailedDeleteBatches(ctx context.Context, tenantID uint64, kbID, taskID string) error
}

type KnowledgeDirectoryService interface {
	Create(ctx context.Context, tenantID uint64, kbID, tagID string, parentID *string, name string) (*types.KnowledgeDirectory, error)
	List(ctx context.Context, tenantID uint64, kbID string, parentID *string, page *types.Pagination, sortBy, sortOrder string, visibility types.KnowledgeVisibilityFilter, tagIDs ...string) ([]*types.KnowledgeDirectory, int64, error)
	Rename(ctx context.Context, tenantID uint64, kbID, tagID, id, name string) error
	Move(ctx context.Context, tenantID uint64, kbID, tagID, id string, parentID *string) error
	MoveEntries(ctx context.Context, tenantID uint64, kbID, tagID string, directoryIDs, knowledgeIDs []string, parentID *string) error
	MoveSubtreesToTag(ctx context.Context, tenantID uint64, kbID, sourceTagID, targetTagID string, directoryIDs, knowledgeIDs []string) ([]string, error)
	DeleteEmpty(ctx context.Context, tenantID uint64, kbID, tagID, id string) error
	Breadcrumb(ctx context.Context, tenantID uint64, kbID, tagID, id string) ([]types.PathNode, error)
	EnsureUploadPath(ctx context.Context, tenantID uint64, kbID string, parentID *string, relativePath string, tagIDs ...string) (*types.KnowledgeDirectory, error)
	ListSubtree(ctx context.Context, tenantID uint64, kbID, tagID, rootID string) ([]*types.KnowledgeDirectory, []*types.Knowledge, error)
	PreviewDelete(ctx context.Context, tenantID uint64, kbID, tagID, rootID, requestedBy string) (*types.KnowledgeDirectoryDeletePreview, error)
	ConfirmDelete(ctx context.Context, tenantID uint64, kbID, tagID, rootID, requestedBy, confirmationToken string) (*types.KnowledgeDirectoryDeleteTask, error)
	GetDeleteTask(ctx context.Context, tenantID uint64, kbID, taskID string) (*types.KnowledgeDirectoryDeleteTask, []*types.KnowledgeDirectoryDeleteBatch, error)
	DispatchPendingDeletes(ctx context.Context) error
	RetryDeleteTask(ctx context.Context, tenantID uint64, kbID, taskID string) error
}
