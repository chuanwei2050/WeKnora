package handler

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

func TestAdminActorRejectsAPIKeyAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest("GET", "/api/v1/admin/tenants", nil)
	ctx := context.WithValue(request.Context(), types.UserContextKey, &types.User{Role: types.UserRolePlatformAdmin})
	ctx = context.WithValue(ctx, types.AuthenticationMethodContextKey, types.AuthenticationMethodAPIKey)
	request = request.WithContext(ctx)
	ginContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	ginContext.Request = request

	if _, ok := adminActor(ginContext); ok {
		t.Fatal("adminActor() accepted API key authentication")
	}
}

func TestTenantHasBusinessUsers(t *testing.T) {
	tests := []struct {
		name  string
		users []*types.User
		total int64
		want  bool
	}{
		{name: "initial tenant admin only", users: []*types.User{{Role: types.UserRoleTenantAdmin}}, total: 1, want: false},
		{name: "member exists", users: []*types.User{{Role: types.UserRoleTenantAdmin}, {Role: types.UserRoleMember}}, total: 2, want: true},
		{name: "more users than page", users: []*types.User{{Role: types.UserRoleTenantAdmin}}, total: 2, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tenantHasBusinessUsers(tt.users, tt.total); got != tt.want {
				t.Fatalf("tenantHasBusinessUsers() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateAdminTenantPassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{name: "empty password is allowed", password: "", wantErr: false},
		{name: "ASCII password is allowed", password: "Admin@123456", wantErr: false},
		{name: "Chinese character is rejected", password: "Admin密码123", wantErr: true},
		{name: "space is rejected", password: "Admin 123456", wantErr: true},
		{name: "tab is rejected", password: "Admin\t123456", wantErr: true},
		{name: "non-ASCII character is rejected", password: "Admin🙂123456", wantErr: true},
		{name: "password without a letter is rejected", password: "12345678!", wantErr: true},
		{name: "password without a number is rejected", password: "Password!", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAdminTenantPassword(tt.password)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateAdminTenantPassword() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
