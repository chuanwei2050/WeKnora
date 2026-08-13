package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type stubModelService struct {
	interfaces.ModelService
	models []*types.Model
	err    error
}

func (s *stubModelService) ListModels(context.Context) ([]*types.Model, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.models, nil
}

func TestGetModelProfileStatusRequiresTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &SystemHandler{modelSvc: &stubModelService{}}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/system/model-profile-status", nil)

	h.GetModelProfileStatus(c)

	require.NotEmpty(t, c.Errors)
	var badReq *errors.AppError
	require.ErrorAs(t, c.Errors.Last().Err, &badReq)
}

func TestGetModelProfileStatusOK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("MODEL_PROFILE", "online")
	t.Setenv("ONLINE_LLM_MODEL_NAME", "demo-chat")
	t.Setenv("ONLINE_LLM_MODEL_BASE_URL", "http://127.0.0.1/v1")

	h := &SystemHandler{modelSvc: &stubModelService{models: []*types.Model{{
		ID:        "m1",
		Name:      "demo-chat",
		Type:      types.ModelTypeKnowledgeQA,
		CreatedAt: time.Unix(1, 0),
	}}}}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/model-profile-status", nil)
	ctx := context.WithValue(req.Context(), types.TenantIDContextKey, uint64(42))
	c.Request = req.WithContext(ctx)

	h.GetModelProfileStatus(c)

	require.Empty(t, c.Errors)
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, 0, body.Code)
	require.NotContains(t, string(body.Data), "api_key")
	require.Contains(t, string(body.Data), `"profile":"online"`)
}
