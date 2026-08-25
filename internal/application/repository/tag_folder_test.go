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
		{ID: "public", TenantID: 1, KnowledgeBaseID: "kb", Name: "Public", IsPublic: true},
		{ID: "child-a", TenantID: 1, KnowledgeBaseID: "kb", Name: "Child A", ParentID: stringPointer("b"), SortOrder: 0},
		{ID: "child-b", TenantID: 1, KnowledgeBaseID: "kb", Name: "Child B", ParentID: stringPointer("b"), SortOrder: 1},
	}
	for _, tag := range tags {
		require.NoError(t, db.Create(tag).Error)
	}
	repo := &knowledgeTagRepository{db: db}

	require.NoError(t, repo.Reorder(context.Background(), 1, "kb", []string{"b", "a"}, []string{"public"}, map[string][]string{"b": {"child-b", "child-a"}}))
	var root, public, child types.KnowledgeTag
	require.NoError(t, db.First(&root, "id = ?", "b").Error)
	require.NoError(t, db.First(&public, "id = ?", "public").Error)
	require.NoError(t, db.First(&child, "id = ?", "child-b").Error)
	require.False(t, root.IsPublic)
	require.Equal(t, 0, root.SortOrder)
	require.True(t, public.IsPublic)
	require.Equal(t, 0, public.SortOrder)
	require.Equal(t, 0, child.SortOrder)

	err = repo.Reorder(context.Background(), 1, "kb", []string{"public", "a"}, []string{"b"}, map[string][]string{"b": {"child-a", "child-b"}})
	require.Error(t, err)
	require.NoError(t, db.First(&root, "id = ?", "b").Error)
	require.NoError(t, db.First(&public, "id = ?", "public").Error)
	require.False(t, root.IsPublic)
	require.True(t, public.IsPublic)
}

func stringPointer(value string) *string {
	return &value
}

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
