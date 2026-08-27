package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type folderKBServiceStub struct {
	interfaces.KnowledgeBaseService
	kb *types.KnowledgeBase
}

func (s folderKBServiceStub) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

type folderTagRepositoryStub struct {
	interfaces.KnowledgeTagRepository
	tags                []*types.KnowledgeTag
	byID                map[string]*types.KnowledgeTag
	rootIDs             []string
	publicIDs           []string
	childOrders         map[string][]string
	updateCall          bool
	deleteCall          bool
	created             *types.KnowledgeTag
	hasChildren         bool
	deleteSubtreeResult bool
}

func (s *folderTagRepositoryStub) GetByName(context.Context, uint64, string, string) (*types.KnowledgeTag, error) {
	return nil, gorm.ErrRecordNotFound
}

func (s *folderTagRepositoryStub) Create(_ context.Context, tag *types.KnowledgeTag) error {
	s.created = tag
	return nil
}

func (s *folderTagRepositoryStub) ListByKB(
	context.Context,
	uint64,
	string,
	*types.Pagination,
	string,
) ([]*types.KnowledgeTag, int64, error) {
	return s.tags, int64(len(s.tags)), nil
}

func (s *folderTagRepositoryStub) Reorder(_ context.Context, _ uint64, _ string, rootIDs, publicIDs []string, childOrders map[string][]string) error {
	s.rootIDs = append([]string(nil), rootIDs...)
	s.publicIDs = append([]string(nil), publicIDs...)
	s.childOrders = childOrders
	return nil
}

func (s *folderTagRepositoryStub) GetByID(_ context.Context, _ uint64, id string) (*types.KnowledgeTag, error) {
	return s.byID[id], nil
}

func (s *folderTagRepositoryStub) Update(context.Context, *types.KnowledgeTag) error {
	s.updateCall = true
	return nil
}

func (s *folderTagRepositoryStub) Delete(context.Context, uint64, string) error {
	s.deleteCall = true
	return nil
}

func (s *folderTagRepositoryStub) HasChildren(context.Context, uint64, string, string) (bool, error) {
	return s.hasChildren, nil
}

func (s *folderTagRepositoryStub) DeleteEmptySubtree(context.Context, uint64, string, string) (bool, error) {
	s.deleteCall = s.deleteSubtreeResult
	return s.deleteSubtreeResult, nil
}

type documentTagServiceStub struct {
	interfaces.KnowledgeTagService
	untagged *types.KnowledgeTag
}

func (s documentTagServiceStub) FindOrCreateTagByName(context.Context, string, string) (*types.KnowledgeTag, error) {
	return s.untagged, nil
}

func TestKnowledgeTagServiceReorderTags(t *testing.T) {
	repo := &folderTagRepositoryStub{tags: []*types.KnowledgeTag{
		{ID: "untagged", Name: types.UntaggedTagName},
		{ID: "a", Name: "A", IsPublic: true},
		{ID: "b", Name: "B"},
	}}
	svc := &knowledgeTagService{
		kbService: folderKBServiceStub{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1}},
		repo:      repo,
	}

	require.NoError(t, svc.ReorderTags(context.Background(), "kb", []string{"b"}, []string{"a"}, nil))
	require.Equal(t, []string{"b"}, repo.rootIDs)
	require.Equal(t, []string{"a"}, repo.publicIDs)
}

func TestKnowledgeTagServiceCreatesPublicFolder(t *testing.T) {
	repo := &folderTagRepositoryStub{}
	svc := &knowledgeTagService{
		kbService: folderKBServiceStub{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1}},
		repo:      repo,
	}

	tag, err := svc.CreateTag(context.Background(), "kb", "共享资料", "", 0, true, nil)
	require.NoError(t, err)
	require.True(t, tag.IsPublic)
	require.Same(t, tag, repo.created)
}

func TestKnowledgeTagServiceCreatesOrdinaryChildFolder(t *testing.T) {
	parentID := "parent"
	repo := &folderTagRepositoryStub{byID: map[string]*types.KnowledgeTag{
		parentID: {ID: parentID, TenantID: 1, KnowledgeBaseID: "kb", Name: "一级"},
	}}
	svc := &knowledgeTagService{
		kbService: folderKBServiceStub{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1}},
		repo:      repo,
	}

	tag, err := svc.CreateTag(context.Background(), "kb", "二级", "", 0, false, &parentID)
	require.NoError(t, err)
	require.NotNil(t, tag.ParentID)
	require.Equal(t, parentID, *tag.ParentID)
	require.False(t, tag.IsPublic)
}

func TestKnowledgeTagServiceRejectsInvalidChildParents(t *testing.T) {
	nestedParentID := "nested"
	tests := []struct {
		name     string
		parentID string
		parent   *types.KnowledgeTag
		isPublic bool
	}{
		{name: "foreign knowledge base", parentID: "foreign", parent: &types.KnowledgeTag{ID: "foreign", TenantID: 1, KnowledgeBaseID: "other", Name: "其他库"}},
		{name: "public folder", parentID: "public", parent: &types.KnowledgeTag{ID: "public", TenantID: 1, KnowledgeBaseID: "kb", Name: "公共", IsPublic: true}},
		{name: "second level", parentID: "nested", parent: &types.KnowledgeTag{ID: "nested", TenantID: 1, KnowledgeBaseID: "kb", Name: "二级", ParentID: &nestedParentID}},
		{name: "untagged", parentID: "untagged", parent: &types.KnowledgeTag{ID: "untagged", TenantID: 1, KnowledgeBaseID: "kb", Name: types.UntaggedTagName}},
		{name: "public child payload", parentID: "root", parent: &types.KnowledgeTag{ID: "root", TenantID: 1, KnowledgeBaseID: "kb", Name: "一级"}, isPublic: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &folderTagRepositoryStub{byID: map[string]*types.KnowledgeTag{tt.parentID: tt.parent}}
			svc := &knowledgeTagService{
				kbService: folderKBServiceStub{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1}},
				repo:      repo,
			}

			_, err := svc.CreateTag(context.Background(), "kb", "子级", "", 0, tt.isPublic, &tt.parentID)
			require.Error(t, err)
			require.Nil(t, repo.created)
		})
	}
}

func TestKnowledgeTagServiceRejectsInvalidReorderSets(t *testing.T) {
	tests := []struct {
		name string
		ids  []string
	}{
		{name: "duplicate", ids: []string{"a", "a"}},
		{name: "missing", ids: []string{"a"}},
		{name: "foreign", ids: []string{"a", "foreign"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &folderTagRepositoryStub{tags: []*types.KnowledgeTag{
				{ID: "untagged", Name: types.UntaggedTagName},
				{ID: "a", Name: "A"},
				{ID: "b", Name: "B"},
			}}
			svc := &knowledgeTagService{
				kbService: folderKBServiceStub{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1}},
				repo:      repo,
			}

			require.Error(t, svc.ReorderTags(context.Background(), "kb", tt.ids, nil, nil))
			require.Empty(t, repo.rootIDs)
		})
	}
}

func TestKnowledgeTagServiceReordersOrdinaryChildrenWithinParent(t *testing.T) {
	parentID := "a"
	repo := &folderTagRepositoryStub{tags: []*types.KnowledgeTag{
		{ID: "untagged", Name: types.UntaggedTagName},
		{ID: "a", Name: "A"},
		{ID: "child-a", Name: "Child A", ParentID: &parentID},
		{ID: "child-b", Name: "Child B", ParentID: &parentID},
		{ID: "public", Name: "Public", IsPublic: true},
	}}
	svc := &knowledgeTagService{
		kbService: folderKBServiceStub{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1}},
		repo:      repo,
	}

	childOrders := map[string][]string{"a": {"child-b", "child-a"}}
	require.NoError(t, svc.ReorderTags(context.Background(), "kb", []string{"a"}, []string{"public"}, childOrders))
	require.Equal(t, []string{"a"}, repo.rootIDs)
	require.Equal(t, []string{"public"}, repo.publicIDs)
	require.Equal(t, childOrders, repo.childOrders)
}

func TestKnowledgeTagServiceRejectsCrossParentChildOrder(t *testing.T) {
	parentA := "a"
	parentB := "b"
	repo := &folderTagRepositoryStub{tags: []*types.KnowledgeTag{
		{ID: "untagged", Name: types.UntaggedTagName},
		{ID: parentA, Name: "A"},
		{ID: parentB, Name: "B"},
		{ID: "child-a", Name: "Child A", ParentID: &parentA},
		{ID: "child-b", Name: "Child B", ParentID: &parentB},
	}}
	svc := &knowledgeTagService{
		kbService: folderKBServiceStub{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1}},
		repo:      repo,
	}

	err := svc.ReorderTags(context.Background(), "kb", []string{"a", "b"}, nil, map[string][]string{
		"a": {"child-b"},
		"b": {"child-a"},
	})
	require.Error(t, err)
	require.Empty(t, repo.childOrders)
}

func TestKnowledgeTagServiceProtectsUntaggedFolder(t *testing.T) {
	untagged := &types.KnowledgeTag{ID: "untagged", Name: types.UntaggedTagName}
	repo := &folderTagRepositoryStub{byID: map[string]*types.KnowledgeTag{"untagged": untagged}}
	svc := &knowledgeTagService{repo: repo}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))

	name := "renamed"
	_, err := svc.UpdateTag(ctx, "untagged", &name, nil, nil, nil)
	require.Error(t, err)
	require.False(t, repo.updateCall)

	err = svc.DeleteTag(ctx, "untagged", true, false, false, nil)
	require.Error(t, err)
	require.False(t, repo.deleteCall)
}

func TestKnowledgeTagServiceRejectsDeletingFolderWithChildren(t *testing.T) {
	tag := &types.KnowledgeTag{ID: "parent", TenantID: 1, KnowledgeBaseID: "kb", Name: "一级"}
	repo := &folderTagRepositoryStub{
		byID:        map[string]*types.KnowledgeTag{"parent": tag},
		hasChildren: true,
	}
	svc := &knowledgeTagService{repo: repo}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))

	err := svc.DeleteTag(ctx, "parent", true, false, false, nil)
	require.Error(t, err)
	require.False(t, repo.deleteCall)
}

func TestKnowledgeTagServiceDeletesEmptySubtree(t *testing.T) {
	tag := &types.KnowledgeTag{ID: "parent", TenantID: 1, KnowledgeBaseID: "kb", Name: "一级"}
	repo := &folderTagRepositoryStub{
		byID:                map[string]*types.KnowledgeTag{"parent": tag},
		deleteSubtreeResult: true,
	}
	svc := &knowledgeTagService{repo: repo}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))

	err := svc.DeleteTag(ctx, "parent", false, false, true, nil)
	require.NoError(t, err)
	require.True(t, repo.deleteCall)
}

func TestKnowledgeTagServiceUpdatesFolderSearchSwitch(t *testing.T) {
	tag := &types.KnowledgeTag{ID: "folder", Name: "Folder", SearchEnabled: true}
	repo := &folderTagRepositoryStub{byID: map[string]*types.KnowledgeTag{"folder": tag}}
	svc := &knowledgeTagService{repo: repo}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	disabled := false

	updated, err := svc.UpdateTag(ctx, "folder", nil, nil, nil, &disabled)

	require.NoError(t, err)
	require.False(t, updated.SearchEnabled)
	require.True(t, repo.updateCall)
}

func TestSearchableTagIDsRequiresEnabledAncestors(t *testing.T) {
	disabledParentID := "disabled-parent"
	enabledParentID := "enabled-parent"
	missingParentID := "missing-parent"
	ids := searchableTagIDs([]*types.KnowledgeTag{
		{ID: disabledParentID, SearchEnabled: false},
		{ID: "hidden-child", ParentID: &disabledParentID, SearchEnabled: true},
		{ID: enabledParentID, SearchEnabled: true},
		{ID: "visible-child", ParentID: &enabledParentID, SearchEnabled: true},
		{ID: "disabled-child", ParentID: &enabledParentID, SearchEnabled: false},
		{ID: "orphan", ParentID: &missingParentID, SearchEnabled: true},
		{ID: "public", IsPublic: true, SearchEnabled: true},
	})

	require.Equal(t, []string{"enabled-parent", "public", "visible-child"}, ids)
}

func TestResolveDocumentTagID(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 1}
	repo := &folderTagRepositoryStub{byID: map[string]*types.KnowledgeTag{
		"folder":  {ID: "folder", KnowledgeBaseID: "kb", TenantID: 1},
		"foreign": {ID: "foreign", KnowledgeBaseID: "other", TenantID: 1},
	}}
	svc := &knowledgeService{
		tagRepo:    repo,
		tagService: documentTagServiceStub{untagged: &types.KnowledgeTag{ID: "untagged"}},
	}

	resolved, err := svc.resolveDocumentTagID(context.Background(), kb, "")
	require.NoError(t, err)
	require.Equal(t, "untagged", resolved)

	resolved, err = svc.resolveDocumentTagID(context.Background(), kb, "folder")
	require.NoError(t, err)
	require.Equal(t, "folder", resolved)

	_, err = svc.resolveDocumentTagID(context.Background(), kb, "foreign")
	require.Error(t, err)
}
