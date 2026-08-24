package service

import (
	"context"
	"testing"

	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type searchTargetKnowledgeBaseService struct {
	interfaces.KnowledgeBaseService
	items []*types.KnowledgeBase
}

func (s searchTargetKnowledgeBaseService) GetKnowledgeBasesByIDsOnly(context.Context, []string) ([]*types.KnowledgeBase, error) {
	return s.items, nil
}

type searchTargetKnowledgeService struct {
	interfaces.KnowledgeService
	items            []*types.Knowledge
	searchableTagIDs []string
}

func (s searchTargetKnowledgeService) GetKnowledgeBatchWithSharedAccess(context.Context, uint64, []string) ([]*types.Knowledge, error) {
	return s.items, nil
}

func (s searchTargetKnowledgeService) SearchableTagIDs(context.Context, uint64, string) ([]string, error) {
	return s.searchableTagIDs, nil
}

func TestBuildSearchTargetsFiltersFoldersOnlyWhenRequested(t *testing.T) {
	service := &sessionService{
		knowledgeBaseService: searchTargetKnowledgeBaseService{items: []*types.KnowledgeBase{{ID: "kb-1", TenantID: 7}}},
		knowledgeService:     searchTargetKnowledgeService{searchableTagIDs: []string{"enabled"}},
	}

	conversationTargets, err := service.buildSearchTargets(context.Background(), 7, []string{"kb-1"}, nil, true)
	require.NoError(t, err)
	require.Equal(t, []string{"enabled"}, conversationTargets[0].TagIDs)

	directSearchTargets, err := service.buildSearchTargets(context.Background(), 7, []string{"kb-1"}, nil, false)
	require.NoError(t, err)
	require.Nil(t, directSearchTargets[0].TagIDs)
}

func TestBuildSearchTargetsAppliesExplicitIntegrationFolders(t *testing.T) {
	service := &sessionService{
		knowledgeBaseService: searchTargetKnowledgeBaseService{items: []*types.KnowledgeBase{{ID: "kb-1", TenantID: 7}}},
		knowledgeService:     searchTargetKnowledgeService{},
	}

	targets, err := service.buildSearchTargets(context.Background(), 7, []string{"kb-1"}, nil, false, []string{"ordinary-1", "public-1"})

	require.NoError(t, err)
	require.Equal(t, []string{"ordinary-1", "public-1"}, targets[0].TagIDs)
}

func TestBuildSearchTargetsRejectsDirectDocumentOutsideExplicitFolders(t *testing.T) {
	service := &sessionService{
		knowledgeBaseService: searchTargetKnowledgeBaseService{items: []*types.KnowledgeBase{{ID: "kb-1", TenantID: 7}}},
		knowledgeService: searchTargetKnowledgeService{items: []*types.Knowledge{
			{ID: "doc-1", KnowledgeBaseID: "kb-1", TenantID: 7, TagID: "outside"},
		}},
	}

	_, err := service.buildSearchTargets(context.Background(), 7, []string{"kb-1"}, []string{"doc-1"}, false, []string{"ordinary-1", "public-1"})

	require.EqualError(t, err, "invalid_knowledge_folder_scope")
}

func TestBuildSearchTargetsSkipsKnowledgeBaseWhenAllFoldersAreDisabled(t *testing.T) {
	service := &sessionService{
		knowledgeBaseService: searchTargetKnowledgeBaseService{items: []*types.KnowledgeBase{{ID: "kb-1", TenantID: 7}}},
		knowledgeService:     searchTargetKnowledgeService{searchableTagIDs: []string{}},
	}

	targets, err := service.buildSearchTargets(context.Background(), 7, []string{"kb-1"}, nil, true)

	require.NoError(t, err)
	require.Empty(t, targets)
}

func TestBuildSearchTargetsRestrictsSearchToRequestedDocuments(t *testing.T) {
	service := &sessionService{
		knowledgeBaseService: searchTargetKnowledgeBaseService{items: []*types.KnowledgeBase{{ID: "kb-1", TenantID: 7}}},
		knowledgeService:     searchTargetKnowledgeService{items: []*types.Knowledge{{ID: "doc-1", KnowledgeBaseID: "kb-1", TenantID: 7}}},
	}

	targets, err := service.buildSearchTargets(context.Background(), 7, []string{"kb-1"}, []string{"doc-1"}, false)

	require.NoError(t, err)
	require.Len(t, targets, 1)
	require.Equal(t, types.SearchTargetTypeKnowledge, targets[0].Type)
	require.Equal(t, "kb-1", targets[0].KnowledgeBaseID)
	require.Equal(t, []string{"doc-1"}, targets[0].KnowledgeIDs)
}

func TestBuildSearchTargetsRejectsDocumentOutsideKnowledgeBaseScope(t *testing.T) {
	service := &sessionService{
		knowledgeBaseService: searchTargetKnowledgeBaseService{items: []*types.KnowledgeBase{{ID: "kb-allowed", TenantID: 7}}},
		knowledgeService:     searchTargetKnowledgeService{items: []*types.Knowledge{{ID: "doc-foreign", KnowledgeBaseID: "kb-foreign", TenantID: 7}}},
	}

	_, err := service.buildSearchTargets(context.Background(), 7, []string{"kb-allowed"}, []string{"doc-foreign"}, false)

	appErr, ok := werrors.IsAppError(err)
	require.True(t, ok)
	require.Equal(t, werrors.ErrForbidden, appErr.Code)
}

func TestBuildSearchTargetsRejectsIncompleteDocumentResolution(t *testing.T) {
	service := &sessionService{
		knowledgeBaseService: searchTargetKnowledgeBaseService{items: []*types.KnowledgeBase{{ID: "kb-1", TenantID: 7}}},
		knowledgeService: searchTargetKnowledgeService{items: []*types.Knowledge{
			{ID: "doc-1", KnowledgeBaseID: "kb-1", TenantID: 7},
			{ID: "doc-1", KnowledgeBaseID: "kb-1", TenantID: 7},
		}},
	}

	_, err := service.buildSearchTargets(context.Background(), 7, []string{"kb-1"}, []string{"doc-1", "doc-2"}, false)

	appErr, ok := werrors.IsAppError(err)
	require.True(t, ok)
	require.Equal(t, werrors.ErrForbidden, appErr.Code)
}
