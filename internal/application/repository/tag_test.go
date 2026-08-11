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
