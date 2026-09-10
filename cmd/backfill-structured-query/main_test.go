package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestLoadLocalDotEnvReplacesBlankInheritedValue(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile(filepath.Join(".", ".env"), []byte("MINIO_ENDPOINT=http://127.0.0.1:9000\n"), 0o600))
	t.Setenv("MINIO_ENDPOINT", "")
	require.NoError(t, loadLocalDotEnv())
	require.Equal(t, "http://127.0.0.1:9000", os.Getenv("MINIO_ENDPOINT"))
}

func TestLoadCandidatesSkipsSubmittedAndHonorsTenantAndLimit(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}))
	require.NoError(t, db.Create([]*types.Knowledge{
		{ID: "old-csv", TenantID: 1, FileName: "old.csv", FileType: "CSV", FilePath: "local://old.csv", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled"},
		{ID: "submitted", TenantID: 1, FileName: "done.xlsx", FileType: "xlsx", FilePath: "local://done.xlsx", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", Metadata: types.JSON(`{"structured_dataset_id":"dataset-1"}`)},
		{ID: "other-tenant", TenantID: 2, FileName: "other.xls", FileType: "xls", FilePath: "local://other.xls", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled"},
		{ID: "not-table", TenantID: 1, FileName: "note.pdf", FileType: "pdf", FilePath: "local://note.pdf", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled"},
		{ID: "processing", TenantID: 1, FileName: "processing.xlsx", FileType: "xlsx", FilePath: "local://processing.xlsx", ParseStatus: types.ParseStatusProcessing, EnableStatus: "enabled"},
		{ID: "disabled", TenantID: 1, FileName: "disabled.xlsx", FileType: "xlsx", FilePath: "local://disabled.xlsx", ParseStatus: types.ParseStatusCompleted, EnableStatus: "disabled"},
	}).Error)

	items, scanned, skipped, err := loadCandidates(context.Background(), db, options{tenantID: 1, workers: 1, limit: 1})
	require.NoError(t, err)
	require.Equal(t, 2, scanned)
	require.Equal(t, 1, skipped)
	require.Len(t, items, 1)
	require.Equal(t, "old-csv", items[0].ID)
}
