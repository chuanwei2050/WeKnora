package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type cloneTagRepositoryStub struct {
	interfaces.KnowledgeTagRepository
	byID    map[string]*types.KnowledgeTag
	created []*types.KnowledgeTag
}

func (s *cloneTagRepositoryStub) GetByID(_ context.Context, _ uint64, id string) (*types.KnowledgeTag, error) {
	return s.byID[id], nil
}

func (s *cloneTagRepositoryStub) Create(_ context.Context, tag *types.KnowledgeTag) error {
	s.created = append(s.created, tag)
	return nil
}

func TestGetOrCreateTagInTargetPreservesDuplicateNamesAsDistinctFolders(t *testing.T) {
	repo := &cloneTagRepositoryStub{byID: map[string]*types.KnowledgeTag{
		"source-a": {ID: "source-a", TenantID: 1, KnowledgeBaseID: "source", Name: "合同", SearchEnabled: true},
		"source-b": {ID: "source-b", TenantID: 1, KnowledgeBaseID: "source", Name: "合同", SearchEnabled: true},
	}}
	svc := &knowledgeService{tagRepo: repo}
	mapping := make(map[string]string)

	targetA := svc.getOrCreateTagInTarget(context.Background(), 1, 2, "target", "source-a", mapping)
	targetB := svc.getOrCreateTagInTarget(context.Background(), 1, 2, "target", "source-b", mapping)

	require.NotEmpty(t, targetA)
	require.NotEmpty(t, targetB)
	require.NotEqual(t, targetA, targetB)
	require.Len(t, repo.created, 2)
	require.Equal(t, "合同", repo.created[0].Name)
	require.Equal(t, "合同", repo.created[1].Name)
}

func TestGetOrCreateTagInTargetMapsParentBySourceID(t *testing.T) {
	parentID := "source-parent"
	repo := &cloneTagRepositoryStub{byID: map[string]*types.KnowledgeTag{
		parentID: {ID: parentID, TenantID: 1, KnowledgeBaseID: "source", Name: "资料", SearchEnabled: true},
		"source-child": {
			ID: "source-child", TenantID: 1, KnowledgeBaseID: "source", Name: "合同",
			ParentID: &parentID, SearchEnabled: true,
		},
	}}
	svc := &knowledgeService{tagRepo: repo}
	mapping := make(map[string]string)

	targetChildID := svc.getOrCreateTagInTarget(context.Background(), 1, 2, "target", "source-child", mapping)

	require.NotEmpty(t, targetChildID)
	require.Len(t, repo.created, 2)
	require.Equal(t, mapping[parentID], *repo.created[1].ParentID)
}

func TestGetOrCreateTagInTargetIsStableAcrossIndependentMappings(t *testing.T) {
	repo := &cloneTagRepositoryStub{byID: map[string]*types.KnowledgeTag{
		"source": {ID: "source", TenantID: 1, KnowledgeBaseID: "source-kb", Name: "合同", SearchEnabled: true},
	}}
	svc := &knowledgeService{tagRepo: repo}

	first := svc.getOrCreateTagInTarget(context.Background(), 1, 2, "target-kb", "source", make(map[string]string))
	repo.byID[first] = repo.created[0]
	second := svc.getOrCreateTagInTarget(context.Background(), 1, 2, "target-kb", "source", make(map[string]string))

	require.Equal(t, first, second)
	require.Len(t, repo.created, 1)
}
