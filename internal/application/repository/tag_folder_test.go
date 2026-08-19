package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestKnowledgeTagRepositoryReorderIsAtomic(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:tag-reorder?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeTag{}))

	tags := []*types.KnowledgeTag{
		{ID: "untagged", TenantID: 1, KnowledgeBaseID: "kb", Name: types.UntaggedTagName, SortOrder: -1},
		{ID: "a", TenantID: 1, KnowledgeBaseID: "kb", Name: "A", SortOrder: 0},
		{ID: "b", TenantID: 1, KnowledgeBaseID: "kb", Name: "B", SortOrder: 1},
	}
	for _, tag := range tags {
		require.NoError(t, db.Create(tag).Error)
	}
	repo := &knowledgeTagRepository{db: db}

	require.NoError(t, repo.Reorder(context.Background(), 1, "kb", []string{"b", "a"}))
	var ordered []types.KnowledgeTag
	require.NoError(t, db.Order("sort_order ASC").Find(&ordered).Error)
	require.Equal(t, []string{"untagged", "b", "a"}, []string{ordered[0].ID, ordered[1].ID, ordered[2].ID})

	err = repo.Reorder(context.Background(), 1, "kb", []string{"a", "missing"})
	require.Error(t, err)
	ordered = nil
	require.NoError(t, db.Order("sort_order ASC").Find(&ordered).Error)
	require.Equal(t, []string{"untagged", "b", "a"}, []string{ordered[0].ID, ordered[1].ID, ordered[2].ID})
}
