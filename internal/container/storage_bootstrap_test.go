package container

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

type storageBootstrapRepository struct {
	settings *types.PlatformSettings
	updates  int
}

func (r *storageBootstrapRepository) GetPlatformSettings(context.Context) (*types.PlatformSettings, error) {
	return r.settings, nil
}

func (r *storageBootstrapRepository) UpdatePlatformSettings(_ context.Context, settings *types.PlatformSettings) error {
	r.settings = settings
	r.updates++
	return nil
}

func TestBootstrapStorageEngineConfigUsesMinIOEnvironment(t *testing.T) {
	t.Setenv("STORAGE_TYPE", "minio")
	t.Setenv("MINIO_BUCKET_NAME", "weknora")
	t.Setenv("MINIO_USE_SSL", "false")
	t.Setenv("LOCAL_STORAGE_BASE_DIR", "/data/files")
	repo := &storageBootstrapRepository{settings: &types.PlatformSettings{ID: 1}}

	if err := bootstrapStorageEngineConfigWithRepository(repo); err != nil {
		t.Fatal(err)
	}
	config := repo.settings.StorageEngineConfig
	if repo.updates != 1 || config == nil || config.DefaultProvider != "minio" || config.MinIO == nil {
		t.Fatalf("unexpected storage bootstrap result: updates=%d config=%+v", repo.updates, config)
	}
	if config.MinIO.Mode != "docker" || config.MinIO.BucketName != "weknora" {
		t.Fatalf("unexpected MinIO bootstrap config: %+v", config.MinIO)
	}
}

func TestBootstrapStorageEngineConfigPreservesExistingSettings(t *testing.T) {
	t.Setenv("STORAGE_TYPE", "minio")
	existing := &types.StorageEngineConfig{DefaultProvider: "local"}
	repo := &storageBootstrapRepository{settings: &types.PlatformSettings{ID: 1, StorageEngineConfig: existing}}

	if err := bootstrapStorageEngineConfigWithRepository(repo); err != nil {
		t.Fatal(err)
	}
	if repo.updates != 0 || repo.settings.StorageEngineConfig != existing {
		t.Fatalf("existing storage settings were overwritten: updates=%d config=%+v", repo.updates, repo.settings.StorageEngineConfig)
	}
}
