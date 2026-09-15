package service

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestFilterRetrievedIndexesByScopeRejectsDisabledFolderLeak(t *testing.T) {
	results := []*types.IndexWithScore{
		{ChunkID: "allowed", KnowledgeBaseID: "kb-a", KnowledgeID: "doc-a", TagID: "enabled-a"},
		{ChunkID: "disabled-folder", KnowledgeBaseID: "kb-a", KnowledgeID: "doc-b", TagID: "disabled-a"},
		{ChunkID: "other-kb", KnowledgeBaseID: "kb-b", KnowledgeID: "doc-c", TagID: "enabled-b"},
	}

	filtered := filterRetrievedIndexesByScope(
		results,
		[]string{"kb-a"},
		nil,
		[]string{"enabled-a"},
	)

	require.Len(t, filtered, 1)
	require.Equal(t, "allowed", filtered[0].ChunkID)
}

func TestFilterRetrievedIndexesByScopePreservesMultiKBScope(t *testing.T) {
	results := []*types.IndexWithScore{
		{ChunkID: "a", KnowledgeBaseID: "kb-a", KnowledgeID: "doc-a", TagID: "enabled-a"},
		{ChunkID: "b", KnowledgeBaseID: "kb-b", KnowledgeID: "doc-b", TagID: "enabled-b"},
		{ChunkID: "disabled", KnowledgeBaseID: "kb-b", KnowledgeID: "doc-c", TagID: "disabled-b"},
	}

	filtered := filterRetrievedIndexesByScope(
		results,
		[]string{"kb-a", "kb-b"},
		nil,
		[]string{"enabled-a", "enabled-b"},
	)

	require.Len(t, filtered, 2)
	require.Equal(t, []string{"a", "b"}, []string{filtered[0].ChunkID, filtered[1].ChunkID})
}

func TestFilterRetrievedIndexesByScopeCombinesExplicitFileAndFolder(t *testing.T) {
	results := []*types.IndexWithScore{
		{ChunkID: "selected", KnowledgeBaseID: "kb", KnowledgeID: "doc-selected", TagID: "enabled"},
		{ChunkID: "wrong-file", KnowledgeBaseID: "kb", KnowledgeID: "doc-other", TagID: "enabled"},
		{ChunkID: "wrong-folder", KnowledgeBaseID: "kb", KnowledgeID: "doc-selected", TagID: "disabled"},
	}

	filtered := filterRetrievedIndexesByScope(
		results,
		[]string{"kb"},
		[]string{"doc-selected"},
		[]string{"enabled"},
	)

	require.Len(t, filtered, 1)
	require.Equal(t, "selected", filtered[0].ChunkID)
}

func TestFilterRetrievedIndexesByScopeTreatsEmptyDimensionsAsUnrestricted(t *testing.T) {
	results := []*types.IndexWithScore{
		{ChunkID: "a", KnowledgeBaseID: "kb-a", KnowledgeID: "doc-a", TagID: "folder-a"},
		{ChunkID: "b", KnowledgeBaseID: "kb-b", KnowledgeID: "doc-b", TagID: "folder-b"},
	}

	filtered := filterRetrievedIndexesByScope(results, nil, nil, nil)

	require.Equal(t, results, filtered)
}

func TestFilterSearchResultsByScopeUsesAuthoritativeHydratedTag(t *testing.T) {
	results := []*types.SearchResult{
		{ID: "allowed", KnowledgeBaseID: "kb", KnowledgeID: "doc-a", TagID: "enabled"},
		{ID: "stale-index-leak", KnowledgeBaseID: "kb", KnowledgeID: "doc-b", TagID: "disabled"},
	}

	filtered := filterSearchResultsByScope(results, []string{"kb"}, nil, []string{"enabled"})

	require.Len(t, filtered, 1)
	require.Equal(t, "allowed", filtered[0].ID)
}
