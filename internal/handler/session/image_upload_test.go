package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	filesvc "github.com/Tencent/WeKnora/internal/application/service/file"
	"github.com/Tencent/WeKnora/internal/types"
)

func TestResolveImageFileServiceUsesPlatformDefault(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("LOCAL_STORAGE_BASE_DIR", baseDir)

	fallbackDir := filepath.Join(t.TempDir(), "startup-default")
	handler := &Handler{fileService: filesvc.NewLocalFileService(fallbackDir)}
	tenant := &types.Tenant{StorageEngineConfig: &types.StorageEngineConfig{
		DefaultProvider: "local",
		Local:           &types.LocalEngineConfig{PathPrefix: "platform-default"},
	}}
	ctx := context.WithValue(t.Context(), types.TenantInfoContextKey, tenant)

	service := handler.resolveImageFileService(ctx)
	if _, err := service.SaveBytes(ctx, []byte("image"), 42, "test.png", false); err != nil {
		t.Fatalf("save image with platform storage: %v", err)
	}

	platformDir := filepath.Join(baseDir, "platform-default", "42", "exports")
	entries, err := os.ReadDir(platformDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("image was not saved to platform default storage %q: entries=%d, err=%v", platformDir, len(entries), err)
	}
	fallbackPath := filepath.Join(fallbackDir, "42", "exports")
	if _, err := os.Stat(fallbackPath); !os.IsNotExist(err) {
		t.Fatalf("image unexpectedly used startup default storage %q", fallbackPath)
	}
}
