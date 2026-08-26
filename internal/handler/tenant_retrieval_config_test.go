package handler

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRetrievalConfigWriteRequiresPlatformAdmin(t *testing.T) {
	tests := []struct {
		name    string
		role    types.UserRole
		allowed bool
	}{
		{name: "platform admin", role: types.UserRolePlatformAdmin, allowed: true},
		{name: "tenant admin", role: types.UserRoleTenantAdmin, allowed: false},
		{name: "member", role: types.UserRoleMember, allowed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			request := httptest.NewRequest("PUT", "/api/v1/tenants/kv/retrieval-config", nil)
			user := &types.User{Role: tt.role}
			ctx.Request = request.WithContext(context.WithValue(request.Context(), types.UserContextKey, user))

			allowed := (&TenantHandler{}).requirePlatformAdmin(ctx)

			require.Equal(t, tt.allowed, allowed)
			if tt.allowed {
				require.Empty(t, ctx.Errors)
			} else {
				require.NotEmpty(t, ctx.Errors)
			}
		})
	}
}
