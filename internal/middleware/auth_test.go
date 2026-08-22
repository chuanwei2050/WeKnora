package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	integrationauth "github.com/Tencent/WeKnora/internal/integration"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

type missingTenantService struct {
	interfaces.TenantService
}

type staticTenantService struct {
	interfaces.TenantService
	tenant *types.Tenant
}

func (s staticTenantService) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	return s.tenant, nil
}

type bearerUserService struct {
	interfaces.UserService
	user *types.User
}

func (s bearerUserService) ValidateToken(context.Context, string) (*types.User, error) {
	return s.user, nil
}

func (missingTenantService) ExtractTenantIDFromAPIKey(string) (uint64, error) {
	return 42, nil
}

func (missingTenantService) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	return nil, nil
}

func TestAuthRejectsAPIKeyWhenTenantLookupReturnsNil(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(Auth(missingTenantService{}, nil, nil))
	router.GET("/protected", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.Header.Set("X-API-Key", "tenant-42-key")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestAuthPrefersBearerTokenOverIntegrationCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenant := &types.Tenant{ID: 42, Status: string(types.TenantStatusActive)}
	tenantService := staticTenantService{TenantService: missingTenantService{}, tenant: tenant}
	userService := bearerUserService{user: &types.User{
		ID:       "platform-admin",
		TenantID: 1,
		IsActive: true,
		Role:     types.UserRolePlatformAdmin,
	}}
	router := gin.New()
	router.Use(Auth(
		tenantService,
		userService,
		&config.Config{Tenant: &config.TenantConfig{EnableCrossTenantAccess: true}},
		&integrationauth.Service{},
	))
	router.GET("/protected", func(c *gin.Context) {
		if tenantID, ok := c.Get(types.TenantIDContextKey.String()); !ok || tenantID != uint64(42) {
			t.Fatalf("tenant ID = %v, want 42", tenantID)
		}
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("X-Tenant-ID", "42")
	request.AddCookie(&http.Cookie{Name: integrationauth.BrowserCookieName, Value: "stale-integration-session"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestAuthAllowsVoiceWebSocketToUseOneTimeTicket(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(Auth(missingTenantService{}, nil, nil))
	router.GET("/api/v1/sessions/:id/voice/ws", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-1/voice/ws?ticket=one-time-ticket", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("voice WebSocket status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestAllowedIntegrationInternalPathLimitsWidgetCapabilities(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		path    string
		allowed bool
	}{
		{name: "list agents", method: http.MethodGet, path: "/api/v1/agents", allowed: true},
		{name: "list models", method: http.MethodGet, path: "/api/v1/models", allowed: true},
		{name: "read conversation config", method: http.MethodGet, path: "/api/v1/tenants/kv/conversation-config", allowed: true},
		{name: "read parser engines", method: http.MethodGet, path: "/api/v1/system/parser-engines", allowed: true},
		{name: "read tenant users for knowledge settings", method: http.MethodGet, path: "/api/v1/admin/tenants/10000/users", allowed: true},
		{name: "cannot list tenant knowledge bases via admin", method: http.MethodGet, path: "/api/v1/admin/tenants/10000/knowledge-bases", allowed: false},
		{name: "cannot list all tenants", method: http.MethodGet, path: "/api/v1/admin/tenants", allowed: false},
		{name: "manage knowledge data source", method: http.MethodPost, path: "/api/v1/datasource", allowed: true},
		{name: "read kb initialization config", method: http.MethodGet, path: "/api/v1/initialization/config/kb-1", allowed: true},
		{name: "update kb initialization config", method: http.MethodPut, path: "/api/v1/initialization/config/kb-1", allowed: true},
		{name: "extract graph relations", method: http.MethodPost, path: "/api/v1/initialization/extract/text-relation", allowed: true},
		{name: "cannot access ollama status", method: http.MethodGet, path: "/api/v1/initialization/ollama/status", allowed: false},
		{name: "transcribe voice", method: http.MethodPost, path: "/api/v1/sessions/session-1/voice/transcribe", allowed: true},
		{name: "cannot create agent", method: http.MethodPost, path: "/api/v1/agents", allowed: false},
		{name: "cannot update config", method: http.MethodPut, path: "/api/v1/tenants/kv/conversation-config", allowed: false},
		{name: "cannot access arbitrary session", method: http.MethodGet, path: "/api/v1/sessions/session-1", allowed: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := allowedIntegrationInternalPath(test.method, test.path); got != test.allowed {
				t.Fatalf("allowedIntegrationInternalPath(%q, %q) = %v, want %v", test.method, test.path, got, test.allowed)
			}
		})
	}
}

func TestRequiredIntegrationInternalScopeProtectsManagementReads(t *testing.T) {
	if got := requiredIntegrationInternalScope(http.MethodGet, "/api/v1/datasource?kb_id=kb-1"); got != "knowledge:write" {
		t.Fatalf("datasource scope = %q, want knowledge:write", got)
	}
	if got := requiredIntegrationInternalScope(http.MethodGet, "/api/v1/admin/tenants/7/users"); got != "knowledge:write" {
		t.Fatalf("tenant users scope = %q, want knowledge:write", got)
	}
	if got := requiredIntegrationInternalScope(http.MethodGet, "/api/v1/knowledge-bases/kb-1"); got != "knowledge:read" {
		t.Fatalf("knowledge read scope = %q, want knowledge:read", got)
	}
	if got := requiredIntegrationInternalScope(http.MethodPut, "/api/v1/initialization/config/kb-1"); got != "knowledge:write" {
		t.Fatalf("initialization config scope = %q, want knowledge:write", got)
	}
}
