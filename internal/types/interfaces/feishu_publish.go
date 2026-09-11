package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// FeishuPublishRepository persists publish configs, snapshots, runs and mappings with tenant scope.
type FeishuPublishRepository interface {
	GetConfigByKB(ctx context.Context, tenantID uint64, kbID string) (*types.FeishuPublishConfig, error)
	UpsertConfig(ctx context.Context, cfg *types.FeishuPublishConfig) error
	SoftDeleteConfig(ctx context.Context, tenantID uint64, kbID string) error

	CreateSnapshot(ctx context.Context, snap *types.FeishuPublishSnapshot) error
	GetSnapshot(ctx context.Context, tenantID uint64, id string) (*types.FeishuPublishSnapshot, error)

	CreateRun(ctx context.Context, run *types.FeishuPublishRun) error
	UpdateRun(ctx context.Context, run *types.FeishuPublishRun) error
	GetRun(ctx context.Context, tenantID uint64, id string) (*types.FeishuPublishRun, error)
	GetRunByDigest(ctx context.Context, tenantID uint64, kbID, targetID, digest string) (*types.FeishuPublishRun, error)
	GetActiveRun(ctx context.Context, tenantID uint64, kbID, targetID string) (*types.FeishuPublishRun, error)
	ListRunsByKB(ctx context.Context, tenantID uint64, kbID string, limit int) ([]*types.FeishuPublishRun, error)

	ListMappings(ctx context.Context, tenantID uint64, kbID, targetID string) ([]*types.FeishuPublishMapping, error)
	GetMapping(ctx context.Context, tenantID uint64, kbID, targetID, sourceKind, sourceID string) (*types.FeishuPublishMapping, error)
	UpsertMapping(ctx context.Context, m *types.FeishuPublishMapping) error
	DeleteMappingsByTarget(ctx context.Context, tenantID uint64, kbID, targetID string) error
}

// FeishuPublishService exposes configuration, discovery, preview/confirm and worker entrypoints.
type FeishuPublishService interface {
	IsEnabled() bool
	GetConfigView(ctx context.Context, tenantID uint64, kbID string) (*types.FeishuPublishConfigView, error)
	DiscoverSpaces(ctx context.Context, tenantID uint64, kbID string, req *types.FeishuPublishDiscoverSpacesRequest) (*types.FeishuPublishDiscoverSpacesResponse, error)
	ListNodes(ctx context.Context, tenantID uint64, kbID string, req *types.FeishuPublishListNodesRequest) (*types.FeishuPublishListNodesResponse, error)
	PreviewSync(ctx context.Context, tenantID uint64, kbID string, req *types.FeishuPublishSyncRequest) (*types.FeishuPublishPreview, error)
	ConfirmSync(ctx context.Context, tenantID uint64, kbID string, req *types.FeishuPublishSyncRequest) (*types.FeishuPublishConfirmResponse, error)
	Unbind(ctx context.Context, tenantID uint64, kbID string) error
	GetRun(ctx context.Context, tenantID uint64, kbID, runID string) (*types.FeishuPublishRun, error)
	ProcessPublish(ctx context.Context, tenantID uint64, runID string) error
}
