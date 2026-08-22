package middleware

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/config"
	integrationauth "github.com/Tencent/WeKnora/internal/integration"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// 无需认证的API列表
var noAuthAPI = map[string][]string{
	"/health":                    {"GET"},
	"/api/v1/auth/register":      {"POST"},
	"/api/v1/auth/login":         {"POST"},
	"/api/v1/auth/bidreview-sso": {"POST"},
	"/api/v1/auth/auto-setup":    {"POST"},
	"/api/v1/auth/oidc/config":   {"GET"},
	"/api/v1/auth/oidc/url":      {"GET"},
	"/api/v1/auth/oidc/callback": {"GET"},
	"/api/v1/auth/refresh":       {"POST"},
}

// 检查请求是否在无需认证的API列表中
func isNoAuthAPI(path string, method string) bool {
	if method == http.MethodGet && strings.HasPrefix(path, "/api/v1/sessions/") && strings.HasSuffix(path, "/voice/ws") {
		return true
	}
	for api, methods := range noAuthAPI {
		// 如果以*结尾，按照前缀匹配，否则按照全路径匹配
		if strings.HasSuffix(api, "*") {
			if strings.HasPrefix(path, strings.TrimSuffix(api, "*")) && slices.Contains(methods, method) {
				return true
			}
		} else if path == api && slices.Contains(methods, method) {
			return true
		}
	}
	return false
}

// canAccessTenant checks if a user can access a target tenant
func canAccessTenant(user *types.User, targetTenantID uint64, cfg *config.Config) bool {
	// 1. 检查功能是否启用
	if cfg == nil || cfg.Tenant == nil || !cfg.Tenant.EnableCrossTenantAccess {
		return false
	}
	// 2. 检查用户权限
	if !user.IsPlatformAdmin() {
		return false
	}
	// 3. 如果目标租户是用户自己的租户，允许访问
	if user.TenantID == targetTenantID {
		return true
	}
	// 4. 用户有跨租户权限，允许访问（具体验证在中间件中完成）
	return true
}

// Auth 认证中间件
func Auth(
	tenantService interfaces.TenantService,
	userService interfaces.UserService,
	cfg *config.Config,
	integrationServices ...*integrationauth.Service,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		// ignore OPTIONS request
		if c.Request.Method == "OPTIONS" {
			c.Next()
			return
		}

		// 检查请求是否在无需认证的API列表中
		if isNoAuthAPI(c.Request.URL.Path, c.Request.Method) {
			c.Next()
			return
		}

		authHeader := c.GetHeader("Authorization")
		bearer := ""
		if strings.HasPrefix(authHeader, "Bearer ") {
			bearer = strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		}

		// Integration browser sessions authenticate /api/v1 and /files for embedded hosts.
		// Prefer an explicit Bearer browser session when present so cross-site embeds still
		// work after browsers block third-party cookies.
		if len(integrationServices) > 0 && integrationServices[0] != nil {
			service := integrationServices[0]
			var (
				sessionToken string
				principal    *integrationauth.Principal
				user         *types.User
				tenant       *types.Tenant
				authErr      error
			)
			if bearer != "" {
				principal, user, tenant, authErr = service.Authenticate(c.Request.Context(), bearer, "browser")
				if authErr == nil && user != nil {
					sessionToken = bearer
				}
			} else if cookie, err := c.Cookie(integrationauth.BrowserCookieName); err == nil && cookie != "" {
				principal, user, tenant, authErr = service.Authenticate(c.Request.Context(), cookie, "browser")
				if authErr != nil || user == nil {
					c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: invalid integration session"})
					c.Abort()
					return
				}
				sessionToken = cookie
			}
			if sessionToken != "" {
				if c.GetHeader("X-Tenant-ID") != "" {
					c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: integration tenant is server-bound"})
					c.Abort()
					return
				}
				if !allowedIntegrationInternalPath(c.Request.Method, c.Request.URL.Path) {
					c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: endpoint is outside integration scope"})
					c.Abort()
					return
				}
				requiredScope := requiredIntegrationInternalScope(c.Request.Method, c.Request.URL.Path)
				if !principal.HasScope(requiredScope) {
					c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: integration scope is missing"})
					c.Abort()
					return
				}
				if c.Request.URL.Path == "/files" && service.AuthorizeFile(c.Request.Context(), principal, strings.TrimSpace(c.Query("file_path"))) != nil {
					c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: file is outside integration scope"})
					c.Abort()
					return
				}
				if !isSafeMethod(c.Request.Method) && service.ValidateCSRF(c.Request.Context(), sessionToken, c.GetHeader("X-CSRF-Token")) != nil {
					c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: CSRF validation failed"})
					c.Abort()
					return
				}
				if !isSafeMethod(c.Request.Method) && service.ValidateBrowserOrigin(c.Request.Context(), sessionToken, c.GetHeader("Origin")) != nil {
					c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: integration origin is invalid"})
					c.Abort()
					return
				}
				c.Set("integrationPrincipal", principal)
				c.Set(types.TenantIDContextKey.String(), principal.TenantID)
				c.Set(types.TenantInfoContextKey.String(), tenant)
				c.Set(types.UserContextKey.String(), user)
				c.Set(types.UserIDContextKey.String(), user.ID)
				ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, principal.TenantID)
				ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
				ctx = context.WithValue(ctx, types.UserContextKey, user)
				ctx = context.WithValue(ctx, types.UserIDContextKey, user.ID)
				ctx = context.WithValue(ctx, types.IntegrationKnowledgeBaseScopeContextKey, principal.KnowledgeBaseIDs)
				c.Request = c.Request.WithContext(ctx)
				c.Next()
				return
			}
		}

		// 尝试JWT Token认证
		if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
			token := strings.TrimPrefix(authHeader, "Bearer ")
			user, err := userService.ValidateToken(c.Request.Context(), token)
			if err == nil && user != nil {
				if !user.IsActive {
					c.JSON(http.StatusForbidden, gin.H{"error": "User account is disabled"})
					c.Abort()
					return
				}
				// JWT Token认证成功
				// 检查是否有跨租户访问请求
				targetTenantID := user.TenantID
				tenantHeader := c.GetHeader("X-Tenant-ID")
				if tenantHeader != "" {
					// 解析目标租户ID
					parsedTenantID, err := strconv.ParseUint(tenantHeader, 10, 64)
					if err == nil {
						// 检查用户是否有跨租户访问权限
						if canAccessTenant(user, parsedTenantID, cfg) {
							// 验证目标租户是否存在
							targetTenant, err := tenantService.GetTenantByID(c.Request.Context(), parsedTenantID)
							if err == nil && targetTenant != nil && targetTenant.Status == string(types.TenantStatusActive) {
								targetTenantID = parsedTenantID
								log.Printf("User %s switching to tenant %d", user.ID, targetTenantID)
							} else {
								log.Printf("Error getting target tenant by ID: %v, tenantID: %d", err, parsedTenantID)
								c.JSON(http.StatusBadRequest, gin.H{
									"error": "Invalid target tenant ID",
								})
								c.Abort()
								return
							}
						} else {
							// 用户没有权限访问目标租户
							log.Printf("User %s attempted to access tenant %d without permission", user.ID, parsedTenantID)
							c.JSON(http.StatusForbidden, gin.H{
								"error": "Forbidden: insufficient permissions to access target tenant",
							})
							c.Abort()
							return
						}
					}
				}

				// 获取租户信息（使用目标租户ID）
				tenant, err := tenantService.GetTenantByID(c.Request.Context(), targetTenantID)
				if err != nil {
					log.Printf("Error getting tenant by ID: %v, tenantID: %d, userID: %s", err, targetTenantID, user.ID)
					c.JSON(http.StatusUnauthorized, gin.H{
						"error": "Unauthorized: invalid tenant",
					})
					c.Abort()
					return
				}
				if tenant.Status != string(types.TenantStatusActive) {
					c.JSON(http.StatusForbidden, gin.H{"error": "Tenant is suspended"})
					c.Abort()
					return
				}

				// 存储用户和租户信息到上下文
				c.Set(types.TenantIDContextKey.String(), targetTenantID)
				c.Set(types.TenantInfoContextKey.String(), tenant)
				c.Set(types.UserContextKey.String(), user)
				c.Set(types.UserIDContextKey.String(), user.ID)
				c.Set(types.AuthenticationMethodContextKey.String(), types.AuthenticationMethodBearer)
				if legacyPrincipal := integrationauth.PrincipalFromBidReview(user); legacyPrincipal != nil {
					c.Set("integrationPrincipal", legacyPrincipal)
				}
				c.Request = c.Request.WithContext(
					context.WithValue(context.WithValue(
						context.WithValue(
							context.WithValue(
								context.WithValue(c.Request.Context(), types.TenantIDContextKey, targetTenantID),
								types.TenantInfoContextKey, tenant,
							),
							types.UserContextKey, user,
						),
						types.UserIDContextKey, user.ID,
					), types.AuthenticationMethodContextKey, types.AuthenticationMethodBearer),
				)
				c.Next()
				return
			}
		}

		// 尝试X-API-Key认证（兼容模式）
		apiKey := c.GetHeader("X-API-Key")
		if apiKey != "" {
			// Get tenant information
			tenantID, err := tenantService.ExtractTenantIDFromAPIKey(apiKey)
			if err != nil {
				c.JSON(http.StatusUnauthorized, gin.H{
					"error": "Unauthorized: invalid API key format",
				})
				c.Abort()
				return
			}

			// Verify API key validity (matches the one in database)
			t, err := tenantService.GetTenantByID(c.Request.Context(), tenantID)
			if err != nil {
				log.Printf("Error getting tenant by ID: %v, tenantID: %d", err, tenantID)
				c.JSON(http.StatusUnauthorized, gin.H{
					"error": "Unauthorized: invalid API key",
				})
				c.Abort()
				return
			}
			if t == nil || subtle.ConstantTimeCompare([]byte(t.APIKey), []byte(apiKey)) != 1 {
				c.JSON(http.StatusUnauthorized, gin.H{
					"error": "Unauthorized: invalid API key",
				})
				c.Abort()
				return
			}
			if t.Status != string(types.TenantStatusActive) {
				c.JSON(http.StatusForbidden, gin.H{"error": "Tenant is suspended"})
				c.Abort()
				return
			}

			// 存储租户和用户信息到上下文
			c.Set(types.TenantIDContextKey.String(), tenantID)
			c.Set(types.TenantInfoContextKey.String(), t)

			ctx := context.WithValue(
				context.WithValue(c.Request.Context(), types.TenantIDContextKey, tenantID),
				types.TenantInfoContextKey, t,
			)

			// 通过 TenantID 关联查询用户；找不到时构造系统虚拟用户，
			// 确保所有依赖 UserContextKey 的下游 handler 正常工作。
			user, err := userService.GetUserByTenantID(c.Request.Context(), tenantID)
			if err != nil || user == nil {
				user = &types.User{
					ID:       fmt.Sprintf("system-%d", tenantID),
					Username: fmt.Sprintf("system-%d", tenantID),
					Email:    fmt.Sprintf("system-%d@api-key.local", tenantID),
					TenantID: tenantID,
					IsActive: true,
				}
				log.Printf("No user found for tenant %d via API key, using synthetic system user %s", tenantID, user.ID)
			}
			c.Set(types.UserContextKey.String(), user)
			c.Set(types.UserIDContextKey.String(), user.ID)
			c.Set(types.AuthenticationMethodContextKey.String(), types.AuthenticationMethodAPIKey)
			ctx = context.WithValue(
				context.WithValue(context.WithValue(ctx, types.UserContextKey, user), types.UserIDContextKey, user.ID),
				types.AuthenticationMethodContextKey, types.AuthenticationMethodAPIKey,
			)

			c.Request = c.Request.WithContext(ctx)
			c.Next()
			return
		}

		// 没有提供任何认证信息
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: missing authentication"})
		c.Abort()
	}
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func requiredIntegrationInternalScope(method, path string) string {
	if path == "/files" {
		return "file:read"
	}
	if isIntegrationVoiceTranscriptionPath(method, path) {
		return "chat:write"
	}
	if strings.HasPrefix(path, "/api/v1/datasource") ||
		(strings.HasPrefix(path, "/api/v1/admin/tenants/") && strings.HasSuffix(path, "/users")) {
		return "knowledge:write"
	}
	if !isSafeMethod(method) {
		return "knowledge:write"
	}
	return "knowledge:read"
}

func allowedIntegrationInternalPath(method, path string) bool {
	if path == "/files" {
		return true
	}
	if isSafeMethod(method) {
		for _, exact := range []string{
			"/api/v1/agents",
			"/api/v1/models",
			"/api/v1/system/model-profile",
			"/api/v1/tenants/kv/conversation-config",
		} {
			if path == exact {
				return true
			}
		}
		for _, prefix := range []string{
			"/api/v1/shared-knowledge-bases",
			"/api/v1/system/parser-engines",
			"/api/v1/system/info",
			"/api/v1/datasource",
		} {
			if path == prefix || strings.HasPrefix(path, prefix) {
				return true
			}
		}
		// Embedded knowledge settings may list tenant users; do not open other admin tenant routes.
		if strings.HasPrefix(path, "/api/v1/admin/tenants/") && strings.HasSuffix(path, "/users") {
			return true
		}
	}
	if isIntegrationVoiceTranscriptionPath(method, path) {
		return true
	}
	for _, prefix := range []string{
		"/api/v1/knowledge-bases",
		"/api/v1/knowledge",
		"/api/v1/chunks",
		"/api/v1/tags",
		"/api/v1/faq",
		"/api/v1/datasource",
		"/api/v1/initialization/config",
		"/api/v1/initialization/extract",
	} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func isIntegrationVoiceTranscriptionPath(method, path string) bool {
	return method == http.MethodPost && strings.HasPrefix(path, "/api/v1/sessions/") && strings.HasSuffix(path, "/voice/transcribe")
}

// GetTenantIDFromContext helper function to get tenant ID from context
func GetTenantIDFromContext(ctx context.Context) (uint64, error) {
	tenantID, ok := ctx.Value("tenantID").(uint64)
	if !ok {
		return 0, errors.New("tenant ID not found in context")
	}
	return tenantID, nil
}
