package service

import (
	"context"
	"testing"

	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type autoTagDataSourceRepoStub struct {
	interfaces.DataSourceRepository
	updated *types.DataSource
}

func (s *autoTagDataSourceRepoStub) Update(_ context.Context, ds *types.DataSource) error {
	s.updated = ds
	return nil
}

type autoTagServiceStub struct {
	interfaces.KnowledgeTagService
	stored       *types.KnowledgeTag
	created      *types.KnowledgeTag
	findByName   bool
	createByName bool
}

func (s *autoTagServiceStub) GetTagByID(_ context.Context, _ string) (*types.KnowledgeTag, error) {
	return s.stored, nil
}

func (s *autoTagServiceStub) FindOrCreateTagByName(context.Context, string, string) (*types.KnowledgeTag, error) {
	s.findByName = true
	return nil, werrors.NewConflictError("文件夹名称不唯一，请提供文件夹ID")
}

func (s *autoTagServiceStub) CreateTag(_ context.Context, kbID, name, _ string, _ int, _ bool, _ *string) (*types.KnowledgeTag, error) {
	s.createByName = true
	s.created = &types.KnowledgeTag{ID: "dedicated-folder", KnowledgeBaseID: kbID, Name: name}
	return s.created, nil
}

func TestResolveDataSourceAutoTagCreatesAndPersistsDedicatedFolderWhenNameIsAmbiguous(t *testing.T) {
	dsRepo := &autoTagDataSourceRepoStub{}
	tagService := &autoTagServiceStub{}
	svc := &DataSourceService{dsRepo: dsRepo, tagService: tagService}
	ds := &types.DataSource{ID: "source", KnowledgeBaseID: "kb", Name: "合同"}

	tagID, err := svc.resolveDataSourceAutoTag(context.Background(), ds)

	require.NoError(t, err)
	require.Equal(t, "dedicated-folder", tagID)
	require.Equal(t, "dedicated-folder", ds.AutoTagID)
	require.Same(t, ds, dsRepo.updated)
	require.True(t, tagService.findByName)
	require.True(t, tagService.createByName)
}

func TestResolveDataSourceAutoTagReusesPersistedFolderID(t *testing.T) {
	dsRepo := &autoTagDataSourceRepoStub{}
	tagService := &autoTagServiceStub{stored: &types.KnowledgeTag{ID: "stored-folder", KnowledgeBaseID: "kb"}}
	svc := &DataSourceService{dsRepo: dsRepo, tagService: tagService}
	ds := &types.DataSource{ID: "source", KnowledgeBaseID: "kb", Name: "合同", AutoTagID: "stored-folder"}

	tagID, err := svc.resolveDataSourceAutoTag(context.Background(), ds)

	require.NoError(t, err)
	require.Equal(t, "stored-folder", tagID)
	require.False(t, tagService.findByName)
	require.False(t, tagService.createByName)
	require.Nil(t, dsRepo.updated)
}
