package handler

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

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
