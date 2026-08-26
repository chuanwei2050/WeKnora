package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type integrationFolderTagRepo struct {
	interfaces.KnowledgeTagRepository
	tags []*types.KnowledgeTag
}

func (r integrationFolderTagRepo) GetByIDs(_ context.Context, tenantID uint64, ids []string) ([]*types.KnowledgeTag, error) {
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	var result []*types.KnowledgeTag
	for _, tag := range r.tags {
		if tag.TenantID == tenantID {
			if _, ok := wanted[tag.ID]; ok {
				result = append(result, tag)
			}
		}
	}
	return result, nil
}

func (r integrationFolderTagRepo) ListByKB(_ context.Context, tenantID uint64, kbID string, _ *types.Pagination, _ string) ([]*types.KnowledgeTag, int64, error) {
	panic("ListByKB pagination must be provided")
}

type paginatedIntegrationFolderTagRepo struct {
	integrationFolderTagRepo
	pageSize int
}

func (r paginatedIntegrationFolderTagRepo) ListByKB(_ context.Context, tenantID uint64, kbID string, page *types.Pagination, _ string) ([]*types.KnowledgeTag, int64, error) {
	var result []*types.KnowledgeTag
	for _, tag := range r.integrationFolderTagRepo.tags {
		if tag.TenantID == tenantID && tag.KnowledgeBaseID == kbID {
			result = append(result, tag)
		}
	}
	total := int64(len(result))
	pageSize := page.PageSize
	if r.pageSize > 0 && r.pageSize < pageSize {
		pageSize = r.pageSize
	}
	start := (page.Page - 1) * pageSize
	if start >= len(result) {
		return nil, total, nil
	}
	end := start + pageSize
	if end > len(result) {
		end = len(result)
	}
	return result[start:end], total, nil
}

type integrationFolderKnowledgeRepo struct {
	interfaces.KnowledgeRepository
	knowledges []*types.Knowledge
}

func (r integrationFolderKnowledgeRepo) GetKnowledgeBatch(_ context.Context, tenantID uint64, ids []string) ([]*types.Knowledge, error) {
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	var result []*types.Knowledge
	for _, knowledge := range r.knowledges {
		if knowledge.TenantID == tenantID {
			if _, ok := wanted[knowledge.ID]; ok {
				result = append(result, knowledge)
			}
		}
	}
	return result, nil
}

func integrationFolderServiceFixture() *knowledgeService {
	ordinaryParentID := "ordinary-1"
	tags := []*types.KnowledgeTag{
		{ID: "uncategorized", TenantID: 1, KnowledgeBaseID: "kb-1", Name: types.UntaggedTagName, SortOrder: -1},
		{ID: "ordinary-1", TenantID: 1, KnowledgeBaseID: "kb-1", Name: "普通一", SortOrder: 1},
		{ID: "ordinary-child", TenantID: 1, KnowledgeBaseID: "kb-1", Name: "普通子级", ParentID: &ordinaryParentID, SortOrder: 1},
		{ID: "public-1", TenantID: 1, KnowledgeBaseID: "kb-1", Name: "公共一", IsPublic: true, SortOrder: 2},
		{ID: "ordinary-2", TenantID: 1, KnowledgeBaseID: "kb-2", Name: "普通二", SortOrder: 1},
		{ID: "public-2", TenantID: 1, KnowledgeBaseID: "kb-2", Name: "公共二", IsPublic: true, SortOrder: 2},
		{ID: "foreign", TenantID: 1, KnowledgeBaseID: "kb-3", Name: "其他库", SortOrder: 1},
	}
	return &knowledgeService{
		tagRepo: paginatedIntegrationFolderTagRepo{integrationFolderTagRepo: integrationFolderTagRepo{tags: tags}},
		repo: integrationFolderKnowledgeRepo{knowledges: []*types.Knowledge{
			{ID: "doc-in", TenantID: 1, KnowledgeBaseID: "kb-1", TagID: "ordinary-1"},
			{ID: "doc-public", TenantID: 1, KnowledgeBaseID: "kb-1", TagID: "public-1"},
			{ID: "doc-out", TenantID: 1, KnowledgeBaseID: "kb-1", TagID: "uncategorized"},
		}},
	}
}

func TestIntegrationFolderScopeReadsAllPages(t *testing.T) {
	service := integrationFolderServiceFixture()
	repo := service.tagRepo.(paginatedIntegrationFolderTagRepo)
	repo.pageSize = 2
	service.tagRepo = repo

	folders, err := service.ListIntegrationFolders(t.Context(), 1, "kb-1")
	require.NoError(t, err)
	require.Equal(t, []string{"ordinary-1", "ordinary-child", "public-1"}, folderIDs(folders))

	ids, err := service.ResolveIntegrationFolderIDs(t.Context(), 1, []string{"kb-1", "kb-2"}, []string{"ordinary-1"}, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"ordinary-1", "ordinary-child", "public-1"}, ids)
}

func TestListIntegrationFoldersReturnsOrdinaryAndPublicFolders(t *testing.T) {
	service := integrationFolderServiceFixture()
	folders, err := service.ListIntegrationFolders(t.Context(), 1, "kb-1")
	require.NoError(t, err)
	require.Equal(t, []string{"ordinary-1", "ordinary-child", "public-1"}, folderIDs(folders))
}

func folderIDs(folders []*types.KnowledgeTag) []string {
	ids := make([]string, 0, len(folders))
	for _, folder := range folders {
		ids = append(ids, folder.ID)
	}
	return ids
}

func TestResolveIntegrationFolderIDsExpandsDescendantsAndMergesPublicFoldersFromExplicitFolderKBs(t *testing.T) {
	service := integrationFolderServiceFixture()
	ids, err := service.ResolveIntegrationFolderIDs(t.Context(), 1, []string{"kb-1", "kb-2"}, []string{"ordinary-1", "ordinary-2", "public-1"}, []string{"doc-in", "doc-public"})
	require.NoError(t, err)
	require.Equal(t, []string{"ordinary-1", "ordinary-2", "ordinary-child", "public-1", "public-2"}, ids)
}

func TestResolveIntegrationFolderIDsRejectsInvalidScopes(t *testing.T) {
	service := integrationFolderServiceFixture()
	for name, ids := range map[string][]string{
		"uncategorized": {"uncategorized"},
		"cross kb":      {"foreign"},
		"unknown":       {"missing"},
		"duplicate":     {"ordinary-1", "ordinary-1"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := service.ResolveIntegrationFolderIDs(t.Context(), 1, []string{"kb-1"}, ids, nil)
			require.EqualError(t, err, "invalid_folder_ids")
		})
	}
}

func TestResolveIntegrationFolderIDsAcceptsOrdinaryChild(t *testing.T) {
	service := integrationFolderServiceFixture()
	ids, err := service.ResolveIntegrationFolderIDs(t.Context(), 1, []string{"kb-1"}, []string{"ordinary-child"}, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"ordinary-child", "public-1"}, ids)
}

func TestResolveIntegrationFolderIDsRejectsDocumentOutsideFinalScope(t *testing.T) {
	service := integrationFolderServiceFixture()
	_, err := service.ResolveIntegrationFolderIDs(t.Context(), 1, []string{"kb-1"}, []string{"ordinary-1"}, []string{"doc-out"})
	require.EqualError(t, err, "invalid_knowledge_folder_scope")
}
