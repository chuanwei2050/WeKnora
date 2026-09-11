package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newFeishuPublishTestRepo(t *testing.T) *feishuPublishRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s-%d?mode=memory&cache=shared", t.Name(), time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&types.FeishuPublishConfig{},
		&types.FeishuPublishSnapshot{},
		&types.FeishuPublishRun{},
		&types.FeishuPublishMapping{},
	))
	// Mirror migration unique indexes used by CreateRun / UpsertMapping.
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uq_feishu_publish_runs_idempotency
		ON feishu_publish_runs (tenant_id, knowledge_base_id, target_id, snapshot_digest)`).Error)
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uq_feishu_publish_runs_active
		ON feishu_publish_runs (tenant_id, knowledge_base_id, target_id)
		WHERE status IN ('queued', 'running')`).Error)
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uq_feishu_publish_snapshots_digest
		ON feishu_publish_snapshots (tenant_id, knowledge_base_id, target_id, digest)`).Error)
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uq_feishu_publish_mappings_source
		ON feishu_publish_mappings (tenant_id, knowledge_base_id, target_id, source_kind, source_id)`).Error)
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uq_feishu_publish_configs_kb
		ON feishu_publish_configs (tenant_id, knowledge_base_id) WHERE deleted_at IS NULL`).Error)
	return &feishuPublishRepository{db: db}
}

func TestFeishuPublishRepoCrossTenantIsolation(t *testing.T) {
	repo := newFeishuPublishTestRepo(t)
	ctx := context.Background()

	cfg := &types.FeishuPublishConfig{
		ID:              uuid.NewString(),
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		TargetID:        "target-1",
		AppID:           "app-a",
		AppSecretCipher: "cipher",
		SpaceID:         "space-1",
		ConnectionStatus: types.FeishuPublishConnectionOK,
	}
	require.NoError(t, repo.UpsertConfig(ctx, cfg))

	snap := &types.FeishuPublishSnapshot{
		ID:              uuid.NewString(),
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		TargetID:        "target-1",
		Digest:          "digest-1",
		Payload:         types.JSON([]byte(`{}`)),
	}
	require.NoError(t, repo.CreateSnapshot(ctx, snap))

	run := &types.FeishuPublishRun{
		ID:              uuid.NewString(),
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		TargetID:        "target-1",
		ConfigID:        cfg.ID,
		SnapshotID:      snap.ID,
		SnapshotDigest:  "digest-1",
		SpaceID:         "space-1",
		Status:          types.FeishuPublishRunQueued,
	}
	require.NoError(t, repo.CreateRun(ctx, run))

	mapping := &types.FeishuPublishMapping{
		ID:              uuid.NewString(),
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		TargetID:        "target-1",
		SourceKind:      types.FeishuPublishSourceKindKnowledge,
		SourceID:        "know-1",
		SpaceID:         "space-1",
		NodeToken:       "node-1",
		Status:          types.FeishuPublishMappingActive,
	}
	require.NoError(t, repo.UpsertMapping(ctx, mapping))

	t.Run("config", func(t *testing.T) {
		got, err := repo.GetConfigByKB(ctx, 2, "kb-1")
		require.NoError(t, err)
		require.Nil(t, got)
	})
	t.Run("snapshot", func(t *testing.T) {
		got, err := repo.GetSnapshot(ctx, 2, snap.ID)
		require.NoError(t, err)
		require.Nil(t, got)
	})
	t.Run("run", func(t *testing.T) {
		got, err := repo.GetRun(ctx, 2, run.ID)
		require.NoError(t, err)
		require.Nil(t, got)
	})
	t.Run("run by digest", func(t *testing.T) {
		got, err := repo.GetRunByDigest(ctx, 2, "kb-1", "target-1", "digest-1")
		require.NoError(t, err)
		require.Nil(t, got)
	})
	t.Run("active run", func(t *testing.T) {
		got, err := repo.GetActiveRun(ctx, 2, "kb-1", "target-1")
		require.NoError(t, err)
		require.Nil(t, got)
	})
	t.Run("mapping", func(t *testing.T) {
		got, err := repo.GetMapping(ctx, 2, "kb-1", "target-1", types.FeishuPublishSourceKindKnowledge, "know-1")
		require.NoError(t, err)
		require.Nil(t, got)
	})
	t.Run("list mappings", func(t *testing.T) {
		got, err := repo.ListMappings(ctx, 2, "kb-1", "target-1")
		require.NoError(t, err)
		require.Empty(t, got)
	})
	t.Run("list runs", func(t *testing.T) {
		got, err := repo.ListRunsByKB(ctx, 2, "kb-1", 10)
		require.NoError(t, err)
		require.Empty(t, got)
	})

	// Same-tenant reads succeed.
	sameCfg, err := repo.GetConfigByKB(ctx, 1, "kb-1")
	require.NoError(t, err)
	require.NotNil(t, sameCfg)
	require.True(t, sameCfg.AppSecretConfigured)

	sameRun, err := repo.GetActiveRun(ctx, 1, "kb-1", "target-1")
	require.NoError(t, err)
	require.NotNil(t, sameRun)
	require.Equal(t, run.ID, sameRun.ID)
}

func TestFeishuPublishRepoCreateRunDigestIdempotency(t *testing.T) {
	repo := newFeishuPublishTestRepo(t)
	ctx := context.Background()

	first := &types.FeishuPublishRun{
		ID:              uuid.NewString(),
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		TargetID:        "target-1",
		ConfigID:        "cfg-1",
		SnapshotID:      "snap-1",
		SnapshotDigest:  "same-digest",
		SpaceID:         "space-1",
		Status:          types.FeishuPublishRunQueued,
	}
	require.NoError(t, repo.CreateRun(ctx, first))

	second := &types.FeishuPublishRun{
		ID:              uuid.NewString(),
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		TargetID:        "target-1",
		ConfigID:        "cfg-1",
		SnapshotID:      "snap-1",
		SnapshotDigest:  "same-digest",
		SpaceID:         "space-1",
		Status:          types.FeishuPublishRunQueued,
	}
	require.NoError(t, repo.CreateRun(ctx, second))
	require.Equal(t, first.ID, second.ID)
}

func TestFeishuPublishRepoCreateSnapshotDigestIdempotency(t *testing.T) {
	repo := newFeishuPublishTestRepo(t)
	ctx := context.Background()

	first := &types.FeishuPublishSnapshot{
		ID:              uuid.NewString(),
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		TargetID:        "target-1",
		Digest:          "same-digest",
		Payload:         types.JSON([]byte(`{"v":1}`)),
	}
	require.NoError(t, repo.CreateSnapshot(ctx, first))

	second := &types.FeishuPublishSnapshot{
		ID:              uuid.NewString(),
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		TargetID:        "target-1",
		Digest:          "same-digest",
		Payload:         types.JSON([]byte(`{"v":2}`)),
	}
	require.NoError(t, repo.CreateSnapshot(ctx, second))
	require.Equal(t, first.ID, second.ID)
	require.JSONEq(t, `{"v":1}`, string(second.Payload))
}

func TestFeishuPublishRepoSoftDeleteConfig(t *testing.T) {
	repo := newFeishuPublishTestRepo(t)
	ctx := context.Background()

	cfg := &types.FeishuPublishConfig{
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		TargetID:        "target-1",
		AppID:           "app",
	}
	require.NoError(t, repo.UpsertConfig(ctx, cfg))
	require.NoError(t, repo.SoftDeleteConfig(ctx, 1, "kb-1"))

	got, err := repo.GetConfigByKB(ctx, 1, "kb-1")
	require.NoError(t, err)
	require.Nil(t, got)

	// Soft-deleted config must not be visible to another tenant either.
	got, err = repo.GetConfigByKB(ctx, 2, "kb-1")
	require.NoError(t, err)
	require.Nil(t, got)
}
