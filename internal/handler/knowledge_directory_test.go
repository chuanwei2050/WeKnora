package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appErrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type directoryTestKBService struct {
	interfaces.KnowledgeBaseService
	kb *types.KnowledgeBase
}

type directoryTestKnowledgeService struct {
	interfaces.KnowledgeService
	items map[string]*types.Knowledge
}

func (s directoryTestKnowledgeService) GetKnowledgeByIDOnly(_ context.Context, id string) (*types.Knowledge, error) {
	item, ok := s.items[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return item, nil
}

func (s directoryTestKBService) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

func TestDirectoryDownloadLimitsUseSafeDefaultsAndPositiveOverrides(t *testing.T) {
	t.Setenv("DIRECTORY_ZIP_MAX_DOCUMENTS", "")
	t.Setenv("DIRECTORY_ZIP_MAX_BYTES", "invalid")
	documents, bytes := directoryDownloadLimits()
	require.Equal(t, defaultMaxDirectoryDownloadDocuments, documents)
	require.Equal(t, defaultMaxDirectoryDownloadBytes, bytes)

	t.Setenv("DIRECTORY_ZIP_MAX_DOCUMENTS", "12")
	t.Setenv("DIRECTORY_ZIP_MAX_BYTES", "4096")
	documents, bytes = directoryDownloadLimits()
	require.Equal(t, 12, documents)
	require.Equal(t, int64(4096), bytes)

	t.Setenv("DIRECTORY_ZIP_MAX_DOCUMENTS", "0")
	t.Setenv("DIRECTORY_ZIP_MAX_BYTES", "-1")
	documents, bytes = directoryDownloadLimits()
	require.Equal(t, defaultMaxDirectoryDownloadDocuments, documents)
	require.Equal(t, defaultMaxDirectoryDownloadBytes, bytes)
}

func TestSafeZipEntryNameRejectsTraversalAndAbsolutePaths(t *testing.T) {
	for _, malicious := range []string{"../secret.txt", "folder/../../secret.txt", "/absolute.txt", "folder\\..\\secret.txt", "bad\x00name"} {
		_, err := safeZipEntryName(malicious)
		require.Error(t, err, malicious)
	}
	name, err := safeZipEntryName("root/子目录/file.txt")
	require.NoError(t, err)
	require.Equal(t, "root/子目录/file.txt", name)
}

func TestPlanDirectoryZipEntriesPreservesHierarchyEmptyDirectoriesAndDuplicates(t *testing.T) {
	rootID, childID := "root", "child"
	directories := []*types.KnowledgeDirectory{{ID: rootID}, {ID: childID, ParentID: &rootID}}
	documents := []*types.Knowledge{
		{ID: "a", DirectoryID: &childID, FileName: "same.txt"},
		{ID: "b", DirectoryID: &childID, FileName: "same.txt"},
	}
	directoryEntries, documentEntries, err := planDirectoryZipEntries(directories, documents, map[string]string{rootID: "root", childID: "root/empty-child"})
	require.NoError(t, err)
	require.Equal(t, "root/", directoryEntries[rootID])
	require.Equal(t, "root/empty-child/", directoryEntries[childID])
	require.Equal(t, "root/empty-child/same.txt", documentEntries["a"])
	require.Equal(t, "root/empty-child/same (2).txt", documentEntries["b"])
}

func TestPlanDirectoryZipEntriesRejectsMaliciousNamesAndMissingAssociation(t *testing.T) {
	directoryID := "root"
	_, _, err := planDirectoryZipEntries([]*types.KnowledgeDirectory{{ID: directoryID}}, nil, map[string]string{directoryID: "../escape"})
	require.Error(t, err)
	_, _, err = planDirectoryZipEntries([]*types.KnowledgeDirectory{{ID: directoryID}}, []*types.Knowledge{{ID: "bad", DirectoryID: &directoryID, FileName: "../secret"}}, map[string]string{directoryID: "root"})
	require.Error(t, err)
	_, _, err = planDirectoryZipEntries([]*types.KnowledgeDirectory{{ID: directoryID}}, []*types.Knowledge{{ID: "orphan", FileName: "file.txt"}}, map[string]string{directoryID: "root"})
	require.Error(t, err)
}

func TestDirectoryDeleteContextRechecksCurrentManagementPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &KnowledgeHandler{kbService: directoryTestKBService{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1}}}
	for name, testCase := range map[string]struct {
		role      types.UserRole
		wantError bool
	}{
		"administrator":     {role: types.UserRoleTenantAdmin},
		"revoked to member": {role: types.UserRoleMember, wantError: true},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.WithValue(t.Context(), types.TenantIDContextKey, uint64(1))
			ctx = context.WithValue(ctx, types.UserIDContextKey, "user")
			ctx = context.WithValue(ctx, types.UserContextKey, &types.User{ID: "user", TenantID: 1, Role: testCase.role})
			request := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-bases/kb/directories/root/delete", nil).WithContext(ctx)
			response := httptest.NewRecorder()
			ginContext, _ := gin.CreateTestContext(response)
			ginContext.Request = request
			ginContext.Params = gin.Params{{Key: "id", Value: "kb"}, {Key: "directory_id", Value: "root"}}
			ginContext.Set(types.TenantIDContextKey.String(), uint64(1))
			ginContext.Set(types.UserIDContextKey.String(), "user")
			_, _, _, _, err := handler.directoryDeleteContext(ginContext)
			if testCase.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateDocumentMoveAccessRequiresSameTenantAndKnowledgeBase(t *testing.T) {
	handler := &KnowledgeHandler{kgService: directoryTestKnowledgeService{items: map[string]*types.Knowledge{
		"ok":       {ID: "ok", TenantID: 1, KnowledgeBaseID: "kb", ParseStatus: types.ParseStatusCompleted},
		"foreign":  {ID: "foreign", TenantID: 1, KnowledgeBaseID: "other", ParseStatus: types.ParseStatusCompleted},
		"tenant":   {ID: "tenant", TenantID: 2, KnowledgeBaseID: "kb", ParseStatus: types.ParseStatusCompleted},
		"deleting": {ID: "deleting", TenantID: 1, KnowledgeBaseID: "kb", ParseStatus: types.ParseStatusDeleting},
	}}}
	require.NoError(t, handler.validateDocumentMoveAccess(t.Context(), 1, "kb", []string{"ok"}))
	for _, ids := range [][]string{{"foreign"}, {"tenant"}, {"deleting"}, {"missing"}, {"ok", "foreign"}} {
		require.Error(t, handler.validateDocumentMoveAccess(t.Context(), 1, "kb", ids), ids)
	}
}

func TestDirectoryErrorDoesNotExposeInternalDetails(t *testing.T) {
	err := directoryError(errors.New("database driver secret: duplicate directory name"))
	require.Equal(t, "A directory with this name already exists", err.(*appErrors.AppError).Message)
	err = directoryError(errors.New("storage token=secret"))
	require.Equal(t, "Document directory operation failed", err.(*appErrors.AppError).Message)
}

func TestPreflightDirectoryDownloadRejectsLimitsAndUnreadableFiles(t *testing.T) {
	items := []*types.Knowledge{{ID: "one", FileSize: 10}, {ID: "two", FileSize: 20}}
	opens := 0
	opener := func(_ context.Context, id string) (io.ReadCloser, string, error) {
		opens++
		if id == "two" {
			return nil, "", errors.New("missing object")
		}
		return io.NopCloser(strings.NewReader("data")), id, nil
	}
	require.ErrorContains(t, preflightDirectoryDownload(t.Context(), items, 1, 100, opener), "limit_exceeded")
	require.Zero(t, opens)
	require.ErrorContains(t, preflightDirectoryDownload(t.Context(), items, 10, 100, opener), "file_unavailable")
	require.Equal(t, 2, opens)
	opens = 0
	require.ErrorContains(t, preflightDirectoryDownload(t.Context(), items, 10, 15, opener), "limit_exceeded")
	require.Zero(t, opens)
}
