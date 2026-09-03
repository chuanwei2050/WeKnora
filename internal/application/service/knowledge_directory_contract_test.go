package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type failingDirectoryMirrorRepo struct {
	interfaces.KnowledgeDirectoryRepository
	target      []*types.KnowledgeDirectory
	ensureCalls int
	deletedIDs  []string
}

type deletingKnowledgeRepo struct{ interfaces.KnowledgeRepository }

func (deletingKnowledgeRepo) GetKnowledgeBatch(context.Context, uint64, []string) ([]*types.Knowledge, error) {
	return []*types.Knowledge{{ID: "active", ParseStatus: types.ParseStatusCompleted}, {ID: "deleting", ParseStatus: types.ParseStatusDeleting}}, nil
}

func (r *failingDirectoryMirrorRepo) ListChildren(_ context.Context, _ uint64, kbID string, parentID *string, offset, limit int, _, _ string, _ types.KnowledgeVisibilityFilter, _ ...string) ([]*types.KnowledgeDirectory, int64, error) {
	var all []*types.KnowledgeDirectory
	if kbID == "source" {
		rootID := "source-root"
		all = []*types.KnowledgeDirectory{{ID: rootID, KnowledgeBaseID: kbID, Name: "root"}, {ID: "source-child", KnowledgeBaseID: kbID, ParentID: &rootID, Name: "child"}}
	} else {
		all = r.target
	}
	var children []*types.KnowledgeDirectory
	for _, item := range all {
		if (item.ParentID == nil) == (parentID == nil) && (item.ParentID == nil || *item.ParentID == *parentID) {
			children = append(children, item)
		}
	}
	total := int64(len(children))
	if offset >= len(children) {
		return nil, total, nil
	}
	end := offset + limit
	if end > len(children) {
		end = len(children)
	}
	return children[offset:end], total, nil
}

func (r *failingDirectoryMirrorRepo) EnsurePath(_ context.Context, tenantID uint64, kbID string, _ *string, segments []string, _ ...string) (*types.KnowledgeDirectory, error) {
	r.ensureCalls++
	if r.ensureCalls == 2 {
		return nil, errors.New("injected mirror failure")
	}
	created := &types.KnowledgeDirectory{ID: "created-root", TenantID: tenantID, KnowledgeBaseID: kbID, Name: segments[len(segments)-1]}
	r.target = append(r.target, created)
	return created, nil
}

func (r *failingDirectoryMirrorRepo) DeleteEmpty(_ context.Context, _ uint64, _ string, id string, _ ...string) error {
	r.deletedIDs = append(r.deletedIDs, id)
	for index, item := range r.target {
		if item.ID == id {
			r.target = append(r.target[:index], r.target[index+1:]...)
			break
		}
	}
	return nil
}

func TestDocumentDirectoryDoesNotEnterChunkOrIndexContract(t *testing.T) {
	chunkType := reflect.TypeOf(types.Chunk{})
	_, chunkHasDirectory := chunkType.FieldByName("DirectoryID")
	require.False(t, chunkHasDirectory)
	indexType := reflect.TypeOf(types.IndexInfo{})
	_, indexHasDirectory := indexType.FieldByName("DirectoryID")
	require.False(t, indexHasDirectory)

	chunk := &types.Chunk{ID: "chunk", KnowledgeID: "knowledge", KnowledgeBaseID: "kb", TagID: "left-category", Content: "body", IsEnabled: true}
	index := documentChunkIndexInfo(chunk, chunk.Content, chunk.ID)
	require.Equal(t, "knowledge", index.KnowledgeID)
	require.Equal(t, "kb", index.KnowledgeBaseID)
	require.Equal(t, "left-category", index.TagID)
}

func TestMirrorKnowledgeDirectoryTreeCleansOnlyNewEmptyDirectoriesOnFailure(t *testing.T) {
	repo := &failingDirectoryMirrorRepo{target: []*types.KnowledgeDirectory{{ID: "preexisting", TenantID: 1, KnowledgeBaseID: "target", Name: "keep"}}}
	service := &knowledgeService{directoryRepo: repo}
	err := service.mirrorKnowledgeDirectoryTree(t.Context(), &types.KnowledgeBase{ID: "source", TenantID: 1}, &types.KnowledgeBase{ID: "target", TenantID: 1})
	require.EqualError(t, err, "injected mirror failure")
	require.Equal(t, []string{"created-root"}, repo.deletedIDs)
	require.Len(t, repo.target, 1)
	require.Equal(t, "preexisting", repo.target[0].ID)
}

func TestExplicitKnowledgeBatchExcludesDeletingDocuments(t *testing.T) {
	service := &knowledgeService{repo: deletingKnowledgeRepo{}}
	items, err := service.GetKnowledgeBatch(t.Context(), 1, []string{"active", "deleting"})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "active", items[0].ID)
}
