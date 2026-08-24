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
	tags       []*types.KnowledgeTag
	byID       map[string]*types.KnowledgeTag
	rootIDs    []string
	publicIDs  []string
	updateCall bool
	deleteCall bool
	created    *types.KnowledgeTag
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

func (s *folderTagRepositoryStub) Reorder(_ context.Context, _ uint64, _ string, rootIDs, publicIDs []string) error {
	s.rootIDs = append([]string(nil), rootIDs...)
	s.publicIDs = append([]string(nil), publicIDs...)
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
		{ID: "a", Name: "A"},
		{ID: "b", Name: "B"},
	}}
	svc := &knowledgeTagService{
		kbService: folderKBServiceStub{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1}},
		repo:      repo,
	}

	require.NoError(t, svc.ReorderTags(context.Background(), "kb", []string{"b"}, []string{"a"}))
	require.Equal(t, []string{"b"}, repo.rootIDs)
	require.Equal(t, []string{"a"}, repo.publicIDs)
}

func TestKnowledgeTagServiceCreatesPublicFolder(t *testing.T) {
	repo := &folderTagRepositoryStub{}
	svc := &knowledgeTagService{
		kbService: folderKBServiceStub{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1}},
		repo:      repo,
	}

	tag, err := svc.CreateTag(context.Background(), "kb", "共享资料", "", 0, true)
	require.NoError(t, err)
	require.True(t, tag.IsPublic)
	require.Same(t, tag, repo.created)
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

			require.Error(t, svc.ReorderTags(context.Background(), "kb", tt.ids, nil))
			require.Empty(t, repo.rootIDs)
		})
	}
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

	err = svc.DeleteTag(ctx, "untagged", true, false, nil)
	require.Error(t, err)
	require.False(t, repo.deleteCall)
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
