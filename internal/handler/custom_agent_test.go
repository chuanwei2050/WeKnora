package handler

import (
	"context"
	stderrors "errors"
	"net/http/httptest"
	"testing"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

func TestTenantAdminCannotManagePlatformAgents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &CustomAgentHandler{}
	tests := []struct {
		name   string
		method string
		path   string
		call   func(*gin.Context)
	}{
		{name: "create", method: "POST", path: "/api/v1/agents", call: handler.CreateAgent},
		{name: "update", method: "PUT", path: "/api/v1/agents/test", call: handler.UpdateAgent},
		{name: "delete", method: "DELETE", path: "/api/v1/agents/test", call: handler.DeleteAgent},
		{name: "copy", method: "POST", path: "/api/v1/agents/test/copy", call: handler.CopyAgent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, tt.path, nil)
			ctx := context.WithValue(request.Context(), types.UserContextKey, &types.User{Role: types.UserRoleTenantAdmin})
			ctx = context.WithValue(ctx, types.AuthenticationMethodContextKey, types.AuthenticationMethodBearer)
			request = request.WithContext(ctx)
			ginContext, _ := gin.CreateTestContext(httptest.NewRecorder())
			ginContext.Request = request
			ginContext.Params = gin.Params{{Key: "id", Value: "test"}}

			tt.call(ginContext)

			if len(ginContext.Errors) != 1 {
				t.Fatalf("errors = %d, want 1", len(ginContext.Errors))
			}
			var appError *apperrors.AppError
			if !stderrors.As(ginContext.Errors[0].Err, &appError) || appError.Code != apperrors.ErrForbidden {
				t.Fatalf("error = %v, want forbidden", ginContext.Errors[0].Err)
			}
		})
	}
}
