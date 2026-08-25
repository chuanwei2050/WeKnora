package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type folderHandlerTagServiceStub struct {
	interfaces.KnowledgeTagService
	rootIDs     []string
	publicIDs   []string
	childOrders map[string][]string
	parentID    *string
}

func (s *folderHandlerTagServiceStub) CreateTag(
	_ context.Context,
	_ string,
	name string,
	_ string,
	_ int,
	_ bool,
	parentID *string,
) (*types.KnowledgeTag, error) {
	s.parentID = parentID
	return &types.KnowledgeTag{ID: "child", Name: name, ParentID: parentID}, nil
}

func (s *folderHandlerTagServiceStub) ReorderTags(_ context.Context, _ string, rootIDs, publicIDs []string, childOrders map[string][]string) error {
	s.rootIDs = rootIDs
	s.publicIDs = publicIDs
	s.childOrders = childOrders
	return nil
}

type folderHandlerKBServiceStub struct {
	interfaces.KnowledgeBaseService
	kb *types.KnowledgeBase
}

func (s folderHandlerKBServiceStub) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

func TestFolderReorderRequiresEditorPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("PUT", "/api/v1/knowledge-bases/kb/tags/reorder", nil)
	c.Set(types.TenantIDContextKey.String(), uint64(1))
	c.Set(types.UserIDContextKey.String(), "member")

	h := &TagHandler{kbService: folderHandlerKBServiceStub{kb: &types.KnowledgeBase{
		ID:        "kb",
		TenantID:  1,
		CreatedBy: "owner",
	}}}

	_, err := h.effectiveCtxForKB(c, "kb", types.OrgRoleEditor)
	require.Error(t, err)
}

func TestFolderReorderParsesRootAndPublicSections(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/knowledge-bases/kb/tags/reorder", strings.NewReader(`{"root_tag_ids":["root"],"public_tag_ids":["shared"],"child_orders":{"root":["child"]}}`))
	requestCtx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, uint64(1))
	requestCtx = context.WithValue(requestCtx, types.UserIDContextKey, "owner")
	requestCtx = context.WithValue(requestCtx, types.UserContextKey, &types.User{ID: "owner", TenantID: 1, Role: types.UserRoleTenantAdmin, BidReviewRole: string(types.UserRoleTenantAdmin)})
	c.Request = c.Request.WithContext(requestCtx)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(types.TenantIDContextKey.String(), uint64(1))
	c.Set(types.UserIDContextKey.String(), "owner")
	c.Params = gin.Params{{Key: "id", Value: "kb"}}

	tagService := &folderHandlerTagServiceStub{}
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 1, CreatedBy: "owner"}
	require.True(t, types.CanManageKnowledgeBase(c.Request.Context(), kb))
	h := &TagHandler{
		kbService:  folderHandlerKBServiceStub{kb: kb},
		tagService: tagService,
	}
	h.ReorderTags(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, []string{"root"}, tagService.rootIDs)
	require.Equal(t, []string{"shared"}, tagService.publicIDs)
	require.Equal(t, map[string][]string{"root": {"child"}}, tagService.childOrders)
}

func TestCreateFolderPassesParentID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-bases/kb/tags", strings.NewReader(`{"name":"二级","parent_id":"parent"}`))
	requestCtx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, uint64(1))
	requestCtx = context.WithValue(requestCtx, types.UserIDContextKey, "owner")
	requestCtx = context.WithValue(requestCtx, types.UserContextKey, &types.User{ID: "owner", TenantID: 1, Role: types.UserRoleTenantAdmin, BidReviewRole: string(types.UserRoleTenantAdmin)})
	c.Request = c.Request.WithContext(requestCtx)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(types.TenantIDContextKey.String(), uint64(1))
	c.Set(types.UserIDContextKey.String(), "owner")
	c.Params = gin.Params{{Key: "id", Value: "kb"}}

	tagService := &folderHandlerTagServiceStub{}
	h := &TagHandler{
		kbService:  folderHandlerKBServiceStub{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1, CreatedBy: "owner"}},
		tagService: tagService,
	}
	h.CreateTag(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, tagService.parentID)
	require.Equal(t, "parent", *tagService.parentID)
}
