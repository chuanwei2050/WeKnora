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
	items []*types.Knowledge
}

func (s searchTargetKnowledgeService) GetKnowledgeBatchWithSharedAccess(context.Context, uint64, []string) ([]*types.Knowledge, error) {
	return s.items, nil
}

func TestBuildSearchTargetsRestrictsSearchToRequestedDocuments(t *testing.T) {
	service := &sessionService{
		knowledgeBaseService: searchTargetKnowledgeBaseService{items: []*types.KnowledgeBase{{ID: "kb-1", TenantID: 7}}},
		knowledgeService:     searchTargetKnowledgeService{items: []*types.Knowledge{{ID: "doc-1", KnowledgeBaseID: "kb-1", TenantID: 7}}},
	}

	targets, err := service.buildSearchTargets(context.Background(), 7, []string{"kb-1"}, []string{"doc-1"})

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

	_, err := service.buildSearchTargets(context.Background(), 7, []string{"kb-allowed"}, []string{"doc-foreign"})

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

	_, err := service.buildSearchTargets(context.Background(), 7, []string{"kb-1"}, []string{"doc-1", "doc-2"})

	appErr, ok := werrors.IsAppError(err)
	require.True(t, ok)
	require.Equal(t, werrors.ErrForbidden, appErr.Code)
}
