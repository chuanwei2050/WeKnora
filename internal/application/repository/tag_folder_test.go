package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestKnowledgeTagRepositoryBatchCountReferencesExcludesDeleting(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:tag-count?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.Chunk{}))

	const tenantID uint64 = 1
	const kbID = "kb"
	const tagID = "folder-a"

	knowledges := []*types.Knowledge{
		{ID: "k1", TenantID: tenantID, KnowledgeBaseID: kbID, TagID: tagID, ParseStatus: types.ParseStatusCompleted},
		{ID: "k2", TenantID: tenantID, KnowledgeBaseID: kbID, TagID: tagID, ParseStatus: types.ParseStatusDeleting},
	}
	for _, knowledge := range knowledges {
		require.NoError(t, db.Create(knowledge).Error)
	}

	repo := &knowledgeTagRepository{db: db}
	counts, err := repo.BatchCountReferences(context.Background(), tenantID, kbID, []string{tagID})
	require.NoError(t, err)
	require.Equal(t, int64(1), counts[tagID].KnowledgeCount)

	kCount, _, err := repo.CountReferences(context.Background(), tenantID, kbID, tagID)
	require.NoError(t, err)
	require.Equal(t, int64(1), kCount)
}
