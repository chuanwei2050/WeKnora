package service

import (
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBuildSnapshotPayload(t *testing.T) {
	parentTag := "tag-public"
	parentDir := "dir-1"
	tags := []*types.KnowledgeTag{
		{ID: "tag-public", Name: "企业信息", IsPublic: true},
		{ID: "tag-child", Name: "子标签", IsPublic: true, ParentID: &parentTag},
		{ID: "tag-root", Name: "其它一级", IsPublic: false},
	}
	dirs := []*types.KnowledgeDirectory{
		{ID: "dir-1", TagID: "tag-public", Name: "Root", Status: types.DirectoryStatusActive},
		{ID: "dir-2", TagID: "tag-public", Name: "Child", ParentID: &parentDir, Status: types.DirectoryStatusActive},
		{ID: "dir-del", TagID: "tag-public", Name: "Deleting", Status: types.DirectoryStatusDeleting},
	}
	dirID := "dir-1"
	knowledge := []*types.Knowledge{
		{
			ID: "k-ok", Title: "OK", FileName: "ok.pdf", FileType: "application/pdf",
			FileSize: 100, FileHash: "hash-ok", FilePath: "tenant/obj/ok", CurrentVersionID: "v1",
			DirectoryID: &dirID, TagID: "tag-public", EnableStatus: "enabled", ParseStatus: types.ParseStatusCompleted,
		},
		{
			ID: "k-tag-only", Title: "TagOnly", FileName: "t.pdf", FileType: "application/pdf",
			FileSize: 10, FileHash: "h", FilePath: "tenant/obj/t", TagID: "tag-root",
			EnableStatus: "enabled", ParseStatus: types.ParseStatusCompleted,
		},
		{
			ID: "k-untagged", Title: "Untagged", FileName: "u.pdf", FileType: "application/pdf",
			FileSize: 10, FileHash: "h", FilePath: "tenant/obj/u",
			EnableStatus: "enabled", ParseStatus: types.ParseStatusCompleted,
		},
		{
			ID: "k-disabled", Title: "Off", FileName: "off.pdf", FileType: "application/pdf",
			FileSize: 10, FileHash: "h", FilePath: "tenant/obj/off",
			EnableStatus: "disabled", ParseStatus: types.ParseStatusCompleted,
		},
		{
			ID: "k-parse-fail", Title: "Fail", FileName: "fail.pdf", FileType: "application/pdf",
			FileSize: 10, FileHash: "h", FilePath: "tenant/obj/fail",
			EnableStatus: "enabled", ParseStatus: types.ParseStatusFailed,
		},
		{
			ID: "k-missing-file", Title: "Missing", FileName: "missing.pdf",
			EnableStatus: "enabled", ParseStatus: types.ParseStatusCompleted, FilePath: "",
		},
	}
	mapped := []*types.FeishuPublishMappedSource{
		{SourceKind: types.FeishuPublishSourceKindKnowledge, SourceID: "k-ok", Status: types.FeishuPublishMappingActive},
		{SourceKind: types.FeishuPublishSourceKindKnowledge, SourceID: "k-gone", Status: types.FeishuPublishMappingActive},
		{SourceKind: types.FeishuPublishSourceKindDirectory, SourceID: "dir-1", Status: types.FeishuPublishMappingActive},
	}
	longBody := strings.Repeat("字", types.FeishuPublishMaxBodyChars+10)
	bodyByID := map[string]string{
		"k-ok": longBody,
	}

	payload, digest, err := BuildSnapshotPayload("广电计量知识库", tags, dirs, knowledge, mapped, bodyByID)
	require.NoError(t, err)
	require.NotEmpty(t, digest)
	require.Equal(t, "广电计量知识库", payload.KnowledgeBaseName)
	require.Len(t, payload.VirtualFolders, 2)
	require.Equal(t, types.FeishuPublishVirtualPublicID, payload.VirtualFolders[0].ID)
	require.Equal(t, types.IntegrationPublicFolderName, payload.VirtualFolders[0].Name)
	require.Equal(t, types.FeishuPublishVirtualUncategorizedID, payload.VirtualFolders[1].ID)

	require.Len(t, payload.Tags, 3)
	byTag := map[string]types.FeishuPublishNavSnap{}
	for _, tag := range payload.Tags {
		byTag[tag.ID] = tag
	}
	require.Equal(t, types.FeishuPublishSourceKindVirtualFolder, byTag["tag-public"].ExpectedParentKind)
	require.Equal(t, types.FeishuPublishVirtualPublicID, byTag["tag-public"].ExpectedParentID)
	require.Equal(t, types.FeishuPublishSourceKindTag, byTag["tag-child"].ExpectedParentKind)
	require.Equal(t, "tag-public", byTag["tag-child"].ExpectedParentID)
	require.Equal(t, types.FeishuPublishSourceKindHome, byTag["tag-root"].ExpectedParentKind)

	require.Len(t, payload.Directories, 2)
	require.Equal(t, "dir-1", payload.Directories[0].ID)
	require.Equal(t, types.FeishuPublishSourceKindTag, payload.Directories[0].ExpectedParentKind)
	require.Equal(t, "tag-public", payload.Directories[0].ExpectedParentID)
	require.Equal(t, "dir-2", payload.Directories[1].ID)
	require.Equal(t, types.FeishuPublishSourceKindDirectory, payload.Directories[1].ExpectedParentKind)
	require.Equal(t, "dir-1", payload.Directories[1].ExpectedParentID)

	byID := map[string]types.FeishuPublishKnowledgeSnap{}
	for _, k := range payload.Knowledge {
		byID[k.ID] = k
	}
	require.Equal(t, types.FeishuPublishEligibilityPresent, byID["k-ok"].Eligibility)
	require.True(t, byID["k-ok"].BodyTruncated)
	require.Equal(t, types.FeishuPublishMaxBodyChars, len([]rune(byID["k-ok"].BodyText)))
	require.Equal(t, types.FeishuPublishSourceKindDirectory, byID["k-ok"].ExpectedParentKind)
	require.Equal(t, "dir-1", byID["k-ok"].ExpectedParentID)
	require.Equal(t, types.FeishuPublishSourceKindTag, byID["k-tag-only"].ExpectedParentKind)
	require.Equal(t, "tag-root", byID["k-tag-only"].ExpectedParentID)
	require.Equal(t, types.FeishuPublishSourceKindVirtualFolder, byID["k-untagged"].ExpectedParentKind)
	require.Equal(t, types.FeishuPublishVirtualUncategorizedID, byID["k-untagged"].ExpectedParentID)

	require.Equal(t, types.FeishuPublishEligibilityTemporarilyIneligible, byID["k-disabled"].Eligibility)
	require.Equal(t, types.FeishuPublishEligibilityTemporarilyIneligible, byID["k-parse-fail"].Eligibility)
	require.Equal(t, types.FeishuPublishEligibilityTemporarilyIneligible, byID["k-missing-file"].Eligibility)
	require.Equal(t, types.FeishuPublishEligibilityDeleted, byID["k-gone"].Eligibility)

	oversized := []*types.Knowledge{{
		ID: "k-huge", Title: "Huge", FileName: "huge.bin", FileType: "application/octet-stream",
		FileSize: types.FeishuPublishMaxUploadBytes + 1, FileHash: "h", FilePath: "tenant/obj/huge",
		EnableStatus: "enabled", ParseStatus: types.ParseStatusCompleted,
	}}
	hugePayload, _, err := BuildSnapshotPayload("KB", nil, nil, oversized, nil, nil)
	require.NoError(t, err)
	require.Len(t, hugePayload.Knowledge, 1)
	require.Equal(t, types.FeishuPublishEligibilityTemporarilyIneligible, hugePayload.Knowledge[0].Eligibility)
	require.Contains(t, hugePayload.Knowledge[0].SkipReason, "file too large")

	require.Len(t, payload.MappedIDs, 3)

	again, digest2, err := BuildSnapshotPayload("广电计量知识库", tags, dirs, knowledge, mapped, bodyByID)
	require.NoError(t, err)
	require.Equal(t, digest, digest2)
	require.Equal(t, payload.Directories, again.Directories)
}

func TestBuildSnapshotPayloadSkipsSoftDeletedKnowledge(t *testing.T) {
	deleted := gorm.DeletedAt{Time: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true}
	knowledge := []*types.Knowledge{{
		ID: "k-del", Title: "Gone", EnableStatus: "enabled",
		ParseStatus: types.ParseStatusCompleted, FilePath: "x", DeletedAt: deleted,
	}}
	payload, _, err := BuildSnapshotPayload("KB", nil, nil, knowledge, nil, nil)
	require.NoError(t, err)
	require.Empty(t, payload.Knowledge)
}

func TestIsFeishuKnowledgeSyncEnabled(t *testing.T) {
	t.Setenv(types.EnvEnableFeishuKnowledgeSync, "")
	require.False(t, types.IsFeishuKnowledgeSyncEnabled())
	require.False(t, IsFeishuKnowledgeSyncEnabled())

	t.Setenv(types.EnvEnableFeishuKnowledgeSync, "true")
	require.True(t, types.IsFeishuKnowledgeSyncEnabled())
	require.True(t, IsFeishuKnowledgeSyncEnabled())
}
