package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCheckKnowledgeExistsIgnoresDeletingRecords(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}))

	const tenantID uint64 = 1
	const kbID = "kb"
	for _, knowledge := range []*types.Knowledge{
		{ID: "completed", TenantID: tenantID, KnowledgeBaseID: kbID, FileHash: "completed-hash", ParseStatus: types.ParseStatusCompleted},
		{ID: "deleting", TenantID: tenantID, KnowledgeBaseID: kbID, FileHash: "deleting-hash", ParseStatus: types.ParseStatusDeleting},
	} {
		require.NoError(t, db.Create(knowledge).Error)
	}

	repo := &knowledgeRepository{db: db}
	exists, knowledge, err := repo.CheckKnowledgeExists(context.Background(), tenantID, kbID, &types.KnowledgeCheckParams{
		Type:     "file",
		FileHash: "completed-hash",
	})
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, "completed", knowledge.ID)

	exists, knowledge, err = repo.CheckKnowledgeExists(context.Background(), tenantID, kbID, &types.KnowledgeCheckParams{
		Type:     "file",
		FileHash: "deleting-hash",
	})
	require.NoError(t, err)
	require.False(t, exists)
	require.Nil(t, knowledge)
}
