package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestListPagedKnowledgeFiltersDescendantFolders(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:knowledge-descendants?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeTag{}, &types.Knowledge{}))

	const tenantID uint64 = 1
	const kbID = "kb"
	tags := []*types.KnowledgeTag{
		{ID: "root", TenantID: tenantID, KnowledgeBaseID: kbID, Name: "Root"},
		{ID: "child", TenantID: tenantID, KnowledgeBaseID: kbID, Name: "Child", ParentID: stringPointer("root")},
		{ID: "other", TenantID: tenantID, KnowledgeBaseID: kbID, Name: "Other"},
	}
	for _, tag := range tags {
		require.NoError(t, db.Create(tag).Error)
	}
	for _, knowledge := range []*types.Knowledge{
		{ID: "root-doc", TenantID: tenantID, KnowledgeBaseID: kbID, TagID: "root", ParseStatus: types.ParseStatusCompleted},
		{ID: "child-doc", TenantID: tenantID, KnowledgeBaseID: kbID, TagID: "child", ParseStatus: types.ParseStatusCompleted},
		{ID: "other-doc", TenantID: tenantID, KnowledgeBaseID: kbID, TagID: "other", ParseStatus: types.ParseStatusCompleted},
	} {
		require.NoError(t, db.Create(knowledge).Error)
	}

	repo := &knowledgeRepository{db: db}
	page := &types.Pagination{Page: 1, PageSize: 10}
	items, total, err := repo.ListPagedKnowledgeByKnowledgeBaseID(context.Background(), tenantID, kbID, page, "root", "", "")
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, items, 2)

	items, total, err = repo.ListPagedKnowledgeByKnowledgeBaseID(context.Background(), tenantID, kbID, page, "other", "", "")
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, "other-doc", items[0].ID)
}

func TestListPagedKnowledgeOrdersByDisplayNameAcrossPages(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}))

	const tenantID uint64 = 1
	const kbID = "kb"
	for _, knowledge := range []*types.Knowledge{
		{ID: "10", TenantID: tenantID, KnowledgeBaseID: kbID, FileName: "10-document.docx", ParseStatus: types.ParseStatusCompleted},
		{ID: "1", TenantID: tenantID, KnowledgeBaseID: kbID, FileName: "1-document.docx", ParseStatus: types.ParseStatusCompleted},
		{ID: "2", TenantID: tenantID, KnowledgeBaseID: kbID, FileName: "2-document.docx", ParseStatus: types.ParseStatusCompleted},
	} {
		require.NoError(t, db.Create(knowledge).Error)
	}

	repo := &knowledgeRepository{db: db}
	firstPage, total, err := repo.ListPagedKnowledgeByKnowledgeBaseID(
		context.Background(), tenantID, kbID, &types.Pagination{Page: 1, PageSize: 2}, "", "", "",
	)
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.Equal(t, []string{"1-document.docx", "2-document.docx"}, []string{firstPage[0].FileName, firstPage[1].FileName})

	secondPage, _, err := repo.ListPagedKnowledgeByKnowledgeBaseID(
		context.Background(), tenantID, kbID, &types.Pagination{Page: 2, PageSize: 2}, "", "", "",
	)
	require.NoError(t, err)
	require.Equal(t, "10-document.docx", secondPage[0].FileName)
}
