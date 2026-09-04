package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type preflightKnowledgeRepository struct {
	interfaces.KnowledgeRepository
	params    *types.KnowledgeCheckParams
	knowledge *types.Knowledge
}

func (r *preflightKnowledgeRepository) CheckKnowledgeExists(
	_ context.Context,
	_ uint64,
	_ string,
	params *types.KnowledgeCheckParams,
) (bool, *types.Knowledge, error) {
	r.params = params
	return r.knowledge != nil, r.knowledge, nil
}

type preflightKnowledgeService struct {
	interfaces.KnowledgeService
	repo interfaces.KnowledgeRepository
}

func (s preflightKnowledgeService) GetRepository() interfaces.KnowledgeRepository {
	return s.repo
}

type preflightKnowledgeBaseService struct {
	interfaces.KnowledgeBaseService
	kb *types.KnowledgeBase
}

func (s preflightKnowledgeBaseService) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

func TestNormalizeFileMD5(t *testing.T) {
	t.Parallel()

	hash, ok := normalizeFileMD5("  D557DE9F9B10BD6A850BB7EE5570B139  ")
	require.True(t, ok)
	require.Equal(t, "d557de9f9b10bd6a850bb7ee5570b139", hash)

	_, ok = normalizeFileMD5("not-an-md5")
	require.False(t, ok)
}

func TestPreflightKnowledgeFileReturnsExactDuplicate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := &preflightKnowledgeRepository{knowledge: &types.Knowledge{ID: "existing"}}
	handler := &KnowledgeHandler{
		kgService: preflightKnowledgeService{repo: repository},
		kbService: preflightKnowledgeBaseService{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1}},
	}

	ctx := context.WithValue(t.Context(), types.TenantIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.UserIDContextKey, "admin")
	ctx = context.WithValue(ctx, types.UserContextKey, &types.User{ID: "admin", TenantID: 1, Role: types.UserRoleTenantAdmin})
	request := httptest.NewRequest(http.MethodGet, "/preflight?file_hash=d557de9f9b10bd6a850bb7ee5570b139", nil).WithContext(ctx)
	response := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(response)
	ginContext.Request = request
	ginContext.Params = gin.Params{{Key: "id", Value: "kb"}}
	ginContext.Set(types.TenantIDContextKey.String(), uint64(1))
	ginContext.Set(types.UserIDContextKey.String(), "admin")

	handler.PreflightKnowledgeFile(ginContext)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "d557de9f9b10bd6a850bb7ee5570b139", repository.params.FileHash)
	var payload struct {
		Data filePreflightResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
	require.True(t, payload.Data.Exists)
	require.Equal(t, "existing", payload.Data.Knowledge.ID)
}
