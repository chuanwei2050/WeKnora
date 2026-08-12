package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestKnowledgeTagRepositoryNextSortOrderAppendsWithinKnowledgeBase(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeTag{}))
	repo := NewKnowledgeTagRepository(db)
	ctx := context.Background()

	for _, tag := range []*types.KnowledgeTag{
		{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb-1", Name: "legacy", SortOrder: 0},
		{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb-1", Name: "first", SortOrder: 100},
		{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb-1", Name: "last", SortOrder: 5400},
		{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb-2", Name: "other", SortOrder: 9900},
	} {
		require.NoError(t, db.Create(tag).Error)
	}

	next, err := repo.NextSortOrder(ctx, 1, "kb-1")
	require.NoError(t, err)
	assert.Equal(t, 5500, next)

	empty, err := repo.NextSortOrder(ctx, 1, "kb-empty")
	require.NoError(t, err)
	assert.Equal(t, 100, empty)
}

func TestKnowledgeTagRepositoryReorderPreservesUnlistedTags(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeTag{}))
	repo := NewKnowledgeTagRepository(db)
	ctx := context.Background()

	tags := []*types.KnowledgeTag{
		{ID: "tag-a", TenantID: 1, KnowledgeBaseID: "kb-1", Name: "A", SortOrder: 100},
		{ID: "tag-b", TenantID: 1, KnowledgeBaseID: "kb-1", Name: "B", SortOrder: 200},
		{ID: "tag-c", TenantID: 1, KnowledgeBaseID: "kb-1", Name: "C", SortOrder: 300},
		{ID: "tag-d", TenantID: 1, KnowledgeBaseID: "kb-1", Name: "D", SortOrder: 400},
	}
	for _, tag := range tags {
		require.NoError(t, db.Create(tag).Error)
	}

	require.NoError(t, repo.Reorder(ctx, 1, "kb-1", []string{"tag-b", "tag-a"}))

	var ordered []types.KnowledgeTag
	require.NoError(t, db.
		Where("tenant_id = ? AND knowledge_base_id = ?", 1, "kb-1").
		Order("sort_order ASC").
		Find(&ordered).Error)
	require.Len(t, ordered, 4)
	assert.Equal(t, []string{"tag-b", "tag-a", "tag-c", "tag-d"}, []string{
		ordered[0].ID,
		ordered[1].ID,
		ordered[2].ID,
		ordered[3].ID,
	})
	assert.Equal(t, []int{100, 200, 300, 400}, []int{
		ordered[0].SortOrder,
		ordered[1].SortOrder,
		ordered[2].SortOrder,
		ordered[3].SortOrder,
	})
}
