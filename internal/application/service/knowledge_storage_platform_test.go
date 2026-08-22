package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestCheckStorageEngineConfiguredIgnoresKnowledgeBaseOverride(t *testing.T) {
	kb := &types.KnowledgeBase{
		StorageProviderConfig: &types.StorageProviderConfig{Provider: "cos"},
	}

	if err := checkStorageEngineConfigured(context.Background(), kb); err == nil {
		t.Fatal("expected missing platform storage configuration to be rejected")
	}

	ctx := context.WithValue(context.Background(), types.TenantInfoContextKey, &types.Tenant{
		StorageEngineConfig: &types.StorageEngineConfig{DefaultProvider: "minio"},
	})
	if err := checkStorageEngineConfigured(ctx, kb); err != nil {
		t.Fatalf("expected platform storage configuration to be accepted: %v", err)
	}
}

func TestBuildStorageConfigUsesPlatformProvider(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.TenantInfoContextKey, &types.Tenant{
		StorageEngineConfig: &types.StorageEngineConfig{
			DefaultProvider: "minio",
			MinIO: &types.MinIOEngineConfig{
				Mode:            "remote",
				Endpoint:        "minio.platform.example:9000",
				AccessKeyID:     "platform-access-key",
				SecretAccessKey: "platform-secret-key",
				BucketName:      "platform-bucket",
				PathPrefix:      "platform-prefix",
			},
		},
	})
	kb := &types.KnowledgeBase{
		ID:                    "kb-with-legacy-override",
		StorageProviderConfig: &types.StorageProviderConfig{Provider: "cos"},
		StorageConfig: types.StorageConfig{
			Provider:   "cos",
			BucketName: "legacy-kb-bucket",
			SecretID:   "legacy-secret-id",
		},
	}

	config := (&knowledgeService{}).buildStorageConfig(ctx, kb)
	if config.Provider != "MINIO" {
		t.Fatalf("provider = %q, want MINIO", config.Provider)
	}
	if config.BucketName != "platform-bucket" || config.Endpoint != "minio.platform.example:9000" {
		t.Fatalf("storage config = %#v, want platform MinIO settings", config)
	}
}
