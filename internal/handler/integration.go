package handler

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/handler/session"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	integrationauth "github.com/Tencent/WeKnora/internal/integration"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type IntegrationHandler struct {
	service     *integrationauth.Service
	kbs         interfaces.KnowledgeBaseService
	knowledges  interfaces.KnowledgeService
	sessions    interfaces.SessionService
	messages    interfaces.MessageService
	models      interfaces.ModelService
	agents      interfaces.CustomAgentService
	tenant      interfaces.TenantService
	files       interfaces.FileService
	duckdb      *sql.DB
	streams     interfaces.StreamManager
	attachments *session.AttachmentProcessor
	limiter     *integrationRateLimiter
	limits      integrationLimits
	generations sync.Map
}

// The embedded session is also used by the existing /api/v1 and /files routes.
// A root path keeps those same-origin requests authenticated in both direct and
// reverse-proxy deployments.
const integrationBrowserCookiePath = "/"

func NewIntegrationHandler(service *integrationauth.Service, kbs interfaces.KnowledgeBaseService, knowledges interfaces.KnowledgeService, sessions interfaces.SessionService, messages interfaces.MessageService, streams interfaces.StreamManager, files interfaces.FileService, models interfaces.ModelService, agents interfaces.CustomAgentService, tenant interfaces.TenantService, duckdb *sql.DB, documents interfaces.DocumentReader, imageResolver *docparser.ImageResolver) *IntegrationHandler {
	return &IntegrationHandler{
		service: service, kbs: kbs, knowledges: knowledges, sessions: sessions, messages: messages, models: models, agents: agents, tenant: tenant, files: files, duckdb: duckdb, streams: streams,
		attachments: session.NewAttachmentProcessor(files, documents, imageResolver, models),
		limiter:     newIntegrationRateLimiter(), limits: loadIntegrationLimits(),
	}
}

type integrationLimits struct {
	maxKnowledgeBases int
	maxKnowledgeIDs   int
	maxBatchQueries   int
	batchConcurrency  int
	maxTopK           int
	maxQueryBytes     int
	maxRequestBytes   int
}

func loadIntegrationLimits() integrationLimits {
	positiveEnv := func(name string, fallback int) int {
		value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
		if err != nil || value <= 0 {
			return fallback
		}
		return value
	}
	limits := integrationLimits{
		maxKnowledgeBases: positiveEnv("INTEGRATION_MAX_KNOWLEDGE_BASES", 20),
		maxKnowledgeIDs:   positiveEnv("INTEGRATION_MAX_KNOWLEDGE_IDS", 100),
		maxBatchQueries:   positiveEnv("INTEGRATION_MAX_BATCH_QUERIES", 100),
		batchConcurrency:  positiveEnv("INTEGRATION_BATCH_SEARCH_CONCURRENCY", 8),
		maxTopK:           positiveEnv("INTEGRATION_MAX_TOP_K", 50),
		maxQueryBytes:     positiveEnv("INTEGRATION_MAX_QUERY_BYTES", 8192),
		maxRequestBytes:   positiveEnv("INTEGRATION_MAX_REQUEST_BYTES", 25*1024*1024),
	}
	return limits
}

func (h *IntegrationHandler) retrievalResponseLimit(ctx context.Context, tenantID uint64, requested int) int {
	platformLimit := types.DefaultRerankTopK
	if h.tenant != nil {
		if tenant, err := h.tenant.GetTenantByID(ctx, tenantID); err == nil && tenant != nil {
			platformLimit = tenant.RetrievalConfig.GetEffectiveRerankTopK()
		}
	}
	limit := effectiveIntegrationResponseLimit(requested, platformLimit)
	if h.limits.maxTopK > 0 && limit > h.limits.maxTopK {
		return h.limits.maxTopK
	}
	return limit
}

func effectiveIntegrationResponseLimit(requested, platformLimit int) int {
	if platformLimit <= 0 {
		platformLimit = types.DefaultRerankTopK
	}
	if requested > 0 && requested < platformLimit {
		return requested
	}
	return platformLimit
}

type integrationBatchSearchQuery struct {
	ID                    string    `json:"id" binding:"required"`
	Query                 string    `json:"query" binding:"required"`
	KnowledgeIDs          *[]string `json:"knowledge_ids"`
	FolderIDs             *[]string `json:"folder_ids"`
	FilterDisabledFolders bool      `json:"filter_disabled_folders"`
	TopK                  int       `json:"top_k"`
}

type integrationBatchSearchRequest struct {
	KnowledgeBaseIDs *[]string                     `json:"knowledge_base_ids" binding:"required"`
	Queries          []integrationBatchSearchQuery `json:"queries" binding:"required"`
}

type integrationBatchSearchResult struct {
	ID      string  `json:"id"`
	Status  string  `json:"status"`
	Results []gin.H `json:"results"`
	Error   string  `json:"error,omitempty"`
}

var integrationKnowledgeIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

type integrationFolderProvider interface {
	ListIntegrationFolders(context.Context, uint64, string) ([]*types.KnowledgeTag, error)
	ResolveIntegrationFolderIDs(context.Context, uint64, []string, []string, []string) ([]string, error)
}

type integrationSearchableFolderProvider interface {
	SearchableTagIDs(context.Context, uint64, string) ([]string, error)
}

type folderSearchSession interface {
	SearchKnowledgeWithFolders(context.Context, []string, []string, []string, bool, string) ([]*types.SearchResult, error)
}

func validIntegrationKnowledgeIDs(ids []string, limit int) bool {
	if len(ids) > limit {
		return false
	}
	for _, id := range ids {
		if !integrationKnowledgeIDPattern.MatchString(id) {
			return false
		}
	}
	return true
}

type integrationRateBucket struct {
	started time.Time
	count   int
}
type integrationRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]integrationRateBucket
}

func newIntegrationRateLimiter() *integrationRateLimiter {
	return &integrationRateLimiter{buckets: make(map[string]integrationRateBucket)}
}
func (l *integrationRateLimiter) allow(key string, limit int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.buckets) >= 4096 {
		for bucketKey, candidate := range l.buckets {
			if now.Sub(candidate.started) >= time.Minute {
				delete(l.buckets, bucketKey)
			}
		}
		if _, exists := l.buckets[key]; !exists && len(l.buckets) >= 4096 {
			return false
		}
	}
	bucket := l.buckets[key]
	if bucket.started.IsZero() || now.Sub(bucket.started) >= time.Minute {
		bucket = integrationRateBucket{started: now}
	}
	if bucket.count >= limit {
		l.buckets[key] = bucket
		return false
	}
	bucket.count++
	l.buckets[key] = bucket
	return true
}

func (h *IntegrationHandler) limitRequestBody(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(h.limits.maxRequestBytes))
}
func (h *IntegrationHandler) enforceRate(c *gin.Context, action string, limit int) bool {
	principal := integrationPrincipal(c)
	key := c.ClientIP()
	if principal != nil {
		key = principal.ClientID + ":" + principal.UserID
	}
	if h.limiter.allow(action+":"+key, limit) {
		return true
	}
	h.service.Audit(c.Request.Context(), principal, action, "denied", "rate_limited")
	integrationError(c, http.StatusTooManyRequests, "rate_limited", "too many integration requests")
	return false
}

func (h *IntegrationHandler) audit(c *gin.Context, action, outcome, reason string) {
	h.service.Audit(c.Request.Context(), integrationPrincipal(c), action, outcome, reason)
}

func (h *IntegrationHandler) requireScope(c *gin.Context, scope string) bool {
	if integrationPrincipal(c).HasScope(scope) {
		return true
	}
	h.audit(c, "authorization.scope", "denied", "missing_"+scope)
	integrationError(c, http.StatusForbidden, "scope_denied", "required integration scope is missing")
	return false
}

func integrationError(c *gin.Context, status int, code, message string) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "strict-origin")
	c.JSON(status, gin.H{"success": false, "error": gin.H{"code": code, "message": message}})
}

func integrationData(c *gin.Context, status int, data any) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"success": true, "data": data})
}

func bearerToken(c *gin.Context) string {
	return strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
}

func (h *IntegrationHandler) Token(c *gin.Context) {
	if !h.enforceRate(c, "token", 20) {
		return
	}
	_ = h.service.CleanupExpired(c.Request.Context())
	h.limitRequestBody(c)
	var req struct {
		ClientID     string `json:"client_id" binding:"required"`
		ClientSecret string `json:"client_secret" binding:"required"`
	}
	if c.ShouldBindJSON(&req) != nil {
		integrationError(c, http.StatusBadRequest, "invalid_request", "client_id and client_secret are required")
		return
	}
	token, expires, err := h.service.IssueServiceToken(c.Request.Context(), req.ClientID, req.ClientSecret)
	if err != nil {
		integrationError(c, http.StatusUnauthorized, "invalid_client", "client authentication failed")
		return
	}
	integrationData(c, http.StatusOK, gin.H{"access_token": token, "token_type": "Bearer", "audience": "weknora-integration", "expires_at": expires.UTC().Format(time.RFC3339)})
}

func (h *IntegrationHandler) Bootstrap(c *gin.Context) {
	if !h.enforceRate(c, "bootstrap", 60) {
		return
	}
	h.limitRequestBody(c)
	var req struct {
		ExternalTenantID string   `json:"external_tenant_id" binding:"required"`
		ExternalUserID   string   `json:"external_user_id" binding:"required"`
		ExternalRoles    []string `json:"external_roles"`
		Active           *bool    `json:"active"`
		Origin           string   `json:"origin" binding:"required"`
	}
	if c.ShouldBindJSON(&req) != nil {
		integrationError(c, http.StatusBadRequest, "invalid_request", "external identity and origin are required")
		return
	}
	ticket, err := h.service.CreateBootstrap(c.Request.Context(), bearerToken(c), integrationauth.BootstrapRequest{ExternalTenantID: req.ExternalTenantID, ExternalUserID: req.ExternalUserID, ExternalRoles: req.ExternalRoles, Active: req.Active, Origin: req.Origin})
	if err != nil {
		if errors.Is(err, integrationauth.ErrAdministratorBindingInvalid) {
			h.service.Audit(c.Request.Context(), nil, "auth.bootstrap", "denied", "administrator_binding_invalid")
			integrationError(c, http.StatusForbidden, "administrator_binding_invalid", "integration administrator binding is invalid")
			return
		}
		h.service.Audit(c.Request.Context(), nil, "auth.bootstrap", "denied", "bootstrap_denied")
		integrationError(c, http.StatusForbidden, "bootstrap_denied", "bootstrap request denied")
		return
	}
	integrationData(c, http.StatusCreated, gin.H{"ticket": ticket, "expires_in": int(integrationauth.TicketTTL.Seconds())})
}

func (h *IntegrationHandler) Exchange(c *gin.Context) {
	if !h.enforceRate(c, "exchange", 60) {
		return
	}
	h.limitRequestBody(c)
	var req struct {
		Ticket string `json:"ticket" binding:"required"`
	}
	if c.ShouldBindJSON(&req) != nil {
		integrationError(c, http.StatusBadRequest, "invalid_request", "ticket is required")
		return
	}
	origin := c.GetHeader("Origin")
	token, csrf, principal, user, err := h.service.Exchange(c.Request.Context(), req.Ticket, origin)
	if err != nil {
		h.service.Audit(c.Request.Context(), nil, "auth.exchange", "denied", "ticket_exchange_failed")
		status := http.StatusUnauthorized
		code := "invalid_ticket"
		if errors.Is(err, integrationauth.ErrConflict) {
			status = http.StatusConflict
			code = "ticket_replayed"
		}
		integrationError(c, status, code, "ticket exchange failed")
		return
	}
	c.Header("Access-Control-Allow-Origin", origin)
	c.Header("Access-Control-Allow-Credentials", "true")
	c.Header("Vary", "Origin")
	c.Header("Referrer-Policy", "strict-origin")
	c.SetSameSite(http.SameSiteLaxMode)
	setIntegrationBrowserCookie(c, token, int(integrationauth.SessionMaxTTL.Seconds()))
	aguiEnabled := false
	if h.agents != nil {
		if quickAnswerAgent, agentErr := h.agents.GetAgentByID(c.Request.Context(), types.BuiltinQuickAnswerID); agentErr == nil {
			aguiEnabled = quickAnswerAgent.Config.AGUIEnabled
		}
	}
	// session_token is required for cross-site embeds where browsers block third-party cookies.
	integrationData(c, http.StatusOK, gin.H{
		"csrf_token":         csrf,
		"session_token":      token,
		"user":               user,
		"knowledge_base_ids": principal.KnowledgeBaseIDs,
		"scopes":             principal.Scopes,
		"agui_enabled":       aguiEnabled,
	})
}

func (h *IntegrationHandler) Refresh(c *gin.Context) {
	if !h.enforceRate(c, "refresh", 120) {
		return
	}
	token := integrationBrowserSessionToken(c)
	if token == "" {
		integrationError(c, http.StatusUnauthorized, "missing_session", "session cookie or bearer token is required")
		return
	}
	if err := h.service.ValidateBrowserOrigin(c.Request.Context(), token, c.GetHeader("Origin")); err != nil {
		h.service.Audit(c.Request.Context(), nil, "auth.refresh", "denied", "origin_denied")
		integrationError(c, http.StatusForbidden, "origin_denied", "browser origin denied")
		return
	}
	if err := h.service.ValidateCSRF(c.Request.Context(), token, c.GetHeader("X-CSRF-Token")); err != nil {
		h.service.Audit(c.Request.Context(), nil, "auth.refresh", "denied", "csrf_failed")
		integrationError(c, http.StatusForbidden, "csrf_failed", "CSRF validation failed")
		return
	}
	csrf, user, err := h.service.Refresh(c.Request.Context(), token, c.GetHeader("X-CSRF-Token"))
	if err != nil {
		h.service.Audit(c.Request.Context(), nil, "auth.refresh", "denied", "session_expired")
		integrationError(c, http.StatusUnauthorized, "session_expired", "session refresh failed")
		return
	}
	integrationData(c, http.StatusOK, gin.H{"csrf_token": csrf, "user": user})
}

func (h *IntegrationHandler) Logout(c *gin.Context) {
	token := integrationBrowserSessionToken(c)
	if token != "" {
		if h.service.ValidateBrowserOrigin(c.Request.Context(), token, c.GetHeader("Origin")) != nil || h.service.ValidateCSRF(c.Request.Context(), token, c.GetHeader("X-CSRF-Token")) != nil {
			integrationError(c, http.StatusForbidden, "logout_denied", "origin or CSRF validation failed")
			return
		}
		_ = h.service.Logout(c.Request.Context(), token)
	}
	c.SetSameSite(http.SameSiteLaxMode)
	setIntegrationBrowserCookie(c, "", -1)
	integrationData(c, http.StatusOK, gin.H{"logged_out": true})
}

func integrationCookieSecure(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	if c.Request.TLS != nil {
		return true
	}
	proto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto"))
	if proto == "" {
		proto = strings.TrimSpace(c.GetHeader("X-Forwarded-Protocol"))
	}
	if proto == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(strings.Split(proto, ",")[0]), "https")
}

func setIntegrationBrowserCookie(c *gin.Context, value string, maxAge int) {
	// HTTP LAN / reverse-proxy deployments must not force Secure, or browsers discard the session cookie.
	c.SetCookie(integrationauth.BrowserCookieName, value, maxAge, integrationBrowserCookiePath, "", integrationCookieSecure(c), true)
}

func integrationBrowserSessionToken(c *gin.Context) string {
	if c == nil {
		return ""
	}
	// Prefer explicit Bearer session_token from embedded hosts; cookies may be stale
	// or blocked in third-party iframe contexts.
	if bearer := bearerToken(c); bearer != "" {
		return bearer
	}
	if cookie, err := c.Cookie(integrationauth.BrowserCookieName); err == nil && strings.TrimSpace(cookie) != "" {
		return cookie
	}
	return ""
}

func (h *IntegrationHandler) Authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, kind, ok := resolveIntegrationCredential(c, h.service)
		if !ok {
			integrationError(c, http.StatusUnauthorized, "unauthorized", "integration authentication required")
			c.Abort()
			return
		}
		principal, user, tenant, err := h.service.Authenticate(c.Request.Context(), token, kind)
		if err != nil {
			integrationError(c, http.StatusUnauthorized, "unauthorized", "integration authentication required")
			c.Abort()
			return
		}
		if kind == "service" && c.GetHeader("Origin") != "" {
			integrationError(c, http.StatusForbidden, "service_token_browser_denied", "service tokens cannot be used from a browser origin")
			c.Abort()
			return
		}
		if kind == "browser" && !isReadMethod(c.Request.Method) && h.service.ValidateCSRF(c.Request.Context(), token, c.GetHeader("X-CSRF-Token")) != nil {
			integrationError(c, http.StatusForbidden, "csrf_failed", "CSRF validation failed")
			c.Abort()
			return
		}
		if kind == "browser" && c.GetHeader("Origin") != "" && h.service.ValidateBrowserOrigin(c.Request.Context(), token, c.GetHeader("Origin")) != nil {
			integrationError(c, http.StatusForbidden, "origin_denied", "browser origin denied")
			c.Abort()
			return
		}
		if c.GetHeader("X-Tenant-ID") != "" {
			integrationError(c, http.StatusForbidden, "tenant_override_denied", "integration tenant is server-bound")
			c.Abort()
			return
		}
		setIntegrationContext(c, principal, user, tenant)
		c.Next()
	}
}

func resolveIntegrationCredential(c *gin.Context, service *integrationauth.Service) (token string, kind string, ok bool) {
	if c == nil || service == nil {
		return "", "", false
	}
	cookie, cookieErr := c.Cookie(integrationauth.BrowserCookieName)
	bearer := bearerToken(c)
	candidates := make([]struct {
		token string
		kind  string
	}, 0, 3)
	// Bearer browser sessions win over cookies so CSRF and session stay aligned
	// after embedded hosts exchange a fresh session_token.
	if bearer != "" {
		candidates = append(candidates, struct {
			token string
			kind  string
		}{token: bearer, kind: "browser"})
	}
	if cookieErr == nil && strings.TrimSpace(cookie) != "" {
		candidates = append(candidates, struct {
			token string
			kind  string
		}{token: cookie, kind: "browser"})
	}
	if bearer != "" {
		candidates = append(candidates, struct {
			token string
			kind  string
		}{token: bearer, kind: "service"})
	}
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		key := candidate.kind + "\x00" + candidate.token
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		if _, _, _, err := service.Authenticate(c.Request.Context(), candidate.token, candidate.kind); err == nil {
			return candidate.token, candidate.kind, true
		}
	}
	return "", "", false
}

func isReadMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func setIntegrationContext(c *gin.Context, principal *integrationauth.Principal, user *types.User, tenant *types.Tenant) {
	c.Set("integrationPrincipal", principal)
	c.Set(types.TenantIDContextKey.String(), principal.TenantID)
	c.Set(types.TenantInfoContextKey.String(), tenant)
	if user != nil {
		c.Set(types.UserContextKey.String(), user)
		c.Set(types.UserIDContextKey.String(), user.ID)
	}
	ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, principal.TenantID)
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
	ctx = context.WithValue(ctx, types.UserContextKey, user)
	ctx = context.WithValue(ctx, types.UserIDContextKey, principal.UserID)
	c.Request = c.Request.WithContext(ctx)
}

func integrationPrincipal(c *gin.Context) *integrationauth.Principal {
	value, _ := c.Get("integrationPrincipal")
	principal, _ := value.(*integrationauth.Principal)
	return principal
}

func (h *IntegrationHandler) ListKnowledgeBases(c *gin.Context) {
	if !h.enforceRate(c, "api.knowledge_bases.list", 120) {
		return
	}
	if !h.requireScope(c, "kb:list") {
		return
	}
	principal := integrationPrincipal(c)
	kbs, err := h.kbs.ListKnowledgeBases(c.Request.Context())
	if err != nil {
		integrationError(c, http.StatusInternalServerError, "list_failed", "failed to list knowledge bases")
		return
	}
	allowed := make(map[string]struct{}, len(principal.KnowledgeBaseIDs))
	for _, id := range principal.KnowledgeBaseIDs {
		allowed[id] = struct{}{}
	}
	result := make([]gin.H, 0, len(kbs))
	for _, kb := range kbs {
		if _, ok := allowed[kb.ID]; ok {
			result = append(result, gin.H{"id": kb.ID, "name": kb.Name, "description": kb.Description, "type": kb.Type, "updated_at": kb.UpdatedAt})
		}
	}
	h.audit(c, "api.knowledge_bases.list", "allowed", "")
	integrationData(c, http.StatusOK, result)
}

func (h *IntegrationHandler) ListKnowledgeBaseFolders(c *gin.Context) {
	if !h.requireScope(c, "kb:list") || !h.requireScope(c, "knowledge:read") {
		return
	}
	rawIDs := strings.TrimSpace(c.Query("knowledge_base_ids"))
	if rawIDs == "" {
		h.audit(c, "api.knowledge_base.folders", "denied", "invalid_knowledge_base_ids")
		integrationError(c, http.StatusBadRequest, "invalid_knowledge_base_ids", "knowledge base IDs are required")
		return
	}
	knowledgeBaseIDs := strings.Split(rawIDs, ",")
	for i := range knowledgeBaseIDs {
		knowledgeBaseIDs[i] = strings.TrimSpace(knowledgeBaseIDs[i])
	}
	if len(knowledgeBaseIDs) > h.limits.maxKnowledgeBases || !validIntegrationKnowledgeIDs(knowledgeBaseIDs, h.limits.maxKnowledgeBases) {
		h.audit(c, "api.knowledge_base.folders", "denied", "invalid_knowledge_base_ids")
		integrationError(c, http.StatusBadRequest, "invalid_knowledge_base_ids", "invalid knowledge base IDs")
		return
	}
	principal := integrationPrincipal(c)
	if h.service.AuthorizeKnowledgeBases(principal, knowledgeBaseIDs) != nil {
		h.audit(c, "api.knowledge_base.folders", "denied", "not_found_or_denied")
		integrationError(c, http.StatusNotFound, "knowledge_base_not_found", "knowledge base not found")
		return
	}
	for _, knowledgeBaseID := range knowledgeBaseIDs {
		matched, err := h.kbs.GetRepository().GetKnowledgeBaseByIDAndTenant(c.Request.Context(), knowledgeBaseID, principal.TenantID)
		if err != nil || matched == nil {
			h.audit(c, "api.knowledge_base.folders", "denied", "not_found_or_denied")
			integrationError(c, http.StatusNotFound, "knowledge_base_not_found", "knowledge base not found")
			return
		}
	}
	provider, ok := h.knowledges.(integrationFolderProvider)
	if !ok {
		integrationError(c, http.StatusInternalServerError, "folder_list_failed", "failed to list folders")
		return
	}
	data := make([]gin.H, 0)
	for _, knowledgeBaseID := range knowledgeBaseIDs {
		folders, err := provider.ListIntegrationFolders(c.Request.Context(), principal.TenantID, knowledgeBaseID)
		if err != nil {
			integrationError(c, http.StatusInternalServerError, "folder_list_failed", "failed to list folders")
			return
		}
		for _, folder := range folders {
			data = append(data, gin.H{
				"knowledge_base_id": knowledgeBaseID,
				"id":                folder.ID,
				"name":              folder.Name,
				"parent_id":         folder.ParentID,
				"sort_order":        folder.SortOrder,
			})
		}
	}
	h.service.AuditResources(c.Request.Context(), principal, "api.knowledge_base.folders", "allowed", "", knowledgeBaseIDs)
	integrationData(c, http.StatusOK, data)
}

func (h *IntegrationHandler) CreateKnowledgeBase(c *gin.Context) {
	if !h.enforceRate(c, "api.knowledge_base.create", 30) {
		return
	}
	if !h.requireScope(c, "knowledge:write") {
		return
	}
	h.limitRequestBody(c)
	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
	}
	if c.ShouldBindJSON(&req) != nil {
		h.audit(c, "api.knowledge_base.create", "denied", "invalid_request")
		integrationError(c, http.StatusBadRequest, "invalid_knowledge_base", "name is required")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	if req.Name == "" || len(req.Name) > 255 || len(req.Description) > 4000 {
		h.audit(c, "api.knowledge_base.create", "denied", "limit_exceeded")
		integrationError(c, http.StatusBadRequest, "invalid_knowledge_base", "knowledge base limits exceeded")
		return
	}
	kb, err := h.kbs.CreateKnowledgeBase(c.Request.Context(), &types.KnowledgeBase{
		Name: req.Name, Description: req.Description, Type: types.KnowledgeBaseTypeDocument,
	})
	if err != nil {
		h.audit(c, "api.knowledge_base.create", "denied", "create_failed")
		integrationError(c, http.StatusInternalServerError, "knowledge_base_create_failed", "knowledge base creation failed")
		return
	}
	h.service.AuditResources(c.Request.Context(), integrationPrincipal(c), "api.knowledge_base.create", "allowed", "", []string{kb.ID})
	integrationData(c, http.StatusCreated, kb)
}

func (h *IntegrationHandler) DeleteKnowledgeBase(c *gin.Context) {
	if !h.enforceRate(c, "api.knowledge_base.delete", 30) {
		return
	}
	if !h.requireScope(c, "knowledge:write") {
		return
	}
	kbID := strings.TrimSpace(c.Param("knowledge_base_id"))
	principal := integrationPrincipal(c)
	if kbID == "" || h.service.AuthorizeKnowledgeBases(principal, []string{kbID}) != nil {
		h.service.AuditResources(c.Request.Context(), principal, "api.knowledge_base.delete", "denied", "knowledge_base_denied", []string{kbID})
		integrationError(c, http.StatusForbidden, "knowledge_base_denied", "knowledge base access denied")
		return
	}
	if err := h.kbs.DeleteKnowledgeBase(c.Request.Context(), kbID); err != nil {
		h.service.AuditResources(c.Request.Context(), principal, "api.knowledge_base.delete", "denied", "delete_failed", []string{kbID})
		integrationError(c, http.StatusInternalServerError, "knowledge_base_delete_failed", "knowledge base deletion failed")
		return
	}
	h.service.AuditResources(c.Request.Context(), principal, "api.knowledge_base.delete", "allowed", "", []string{kbID})
	integrationData(c, http.StatusOK, gin.H{"deleted": true, "id": kbID})
}

func (h *IntegrationHandler) CreateKnowledgeFromFile(c *gin.Context) {
	if !h.enforceRate(c, "api.knowledge.create", 120) {
		return
	}
	if !h.requireScope(c, "knowledge:write") {
		return
	}
	h.limitRequestBody(c)
	kbID := strings.TrimSpace(c.Param("knowledge_base_id"))
	principal := integrationPrincipal(c)
	if kbID == "" || h.service.AuthorizeKnowledgeBases(principal, []string{kbID}) != nil {
		h.service.AuditResources(c.Request.Context(), principal, "api.knowledge.create", "denied", "knowledge_base_denied", []string{kbID})
		integrationError(c, http.StatusForbidden, "knowledge_base_denied", "knowledge base access denied")
		return
	}
	file, err := c.FormFile("file")
	if err != nil || file.Size <= 0 {
		h.audit(c, "api.knowledge.create", "denied", "invalid_file")
		integrationError(c, http.StatusBadRequest, "invalid_file", "a non-empty file is required")
		return
	}
	customFileName := strings.TrimSpace(c.PostForm("fileName"))
	if len(customFileName) > 500 {
		integrationError(c, http.StatusBadRequest, "invalid_file", "file name limit exceeded")
		return
	}
	metadata := map[string]string{}
	if raw := strings.TrimSpace(c.PostForm("metadata")); raw != "" {
		if len(raw) > 64*1024 || json.Unmarshal([]byte(raw), &metadata) != nil {
			integrationError(c, http.StatusBadRequest, "invalid_metadata", "metadata must be a string map")
			return
		}
	}
	knowledge, err := h.knowledges.CreateKnowledgeFromFile(
		c.Request.Context(), kbID, file, metadata, nil, customFileName, "", "integration",
	)
	if err != nil {
		fmt.Printf("integration knowledge upload failed for kb %s: %v\n", kbID, err)
		h.service.AuditResources(c.Request.Context(), principal, "api.knowledge.create", "denied", "create_failed", []string{kbID})
		integrationError(c, http.StatusInternalServerError, "knowledge_create_failed", "knowledge creation failed")
		return
	}
	h.service.AuditResources(c.Request.Context(), principal, "api.knowledge.create", "allowed", "", []string{kbID})
	integrationData(c, http.StatusCreated, knowledge)
}

func (h *IntegrationHandler) ListKnowledge(c *gin.Context) {
	if !h.enforceRate(c, "api.knowledge.list", 120) {
		return
	}
	if !h.requireScope(c, "knowledge:read") {
		return
	}
	h.listKnowledge(c, nil, false)
}

func (h *IntegrationHandler) SearchKnowledgeList(c *gin.Context) {
	if !h.enforceRate(c, "api.knowledge.list", 120) {
		return
	}
	if !h.requireScope(c, "knowledge:read") {
		return
	}
	h.limitRequestBody(c)
	var req struct {
		FolderIDs             []string `json:"folder_ids"`
		FilterDisabledFolders bool     `json:"filter_disabled_folders"`
	}
	if c.ShouldBindJSON(&req) != nil || !validIntegrationKnowledgeIDs(req.FolderIDs, h.limits.maxKnowledgeIDs) {
		integrationError(c, http.StatusBadRequest, "invalid_knowledge_filter", "folder_ids and filter_disabled_folders are invalid")
		return
	}
	h.listKnowledge(c, req.FolderIDs, req.FilterDisabledFolders)
}

func (h *IntegrationHandler) listKnowledge(c *gin.Context, folderIDs []string, filterDisabledFolders bool) {
	kbID := strings.TrimSpace(c.Param("knowledge_base_id"))
	principal := integrationPrincipal(c)
	hasFolderFilter := len(folderIDs) > 0
	if kbID == "" || h.service.AuthorizeKnowledgeBases(principal, []string{kbID}) != nil {
		h.audit(c, "api.knowledge.list", "denied", "knowledge_base_denied")
		integrationError(c, http.StatusForbidden, "knowledge_base_denied", "knowledge base access denied")
		return
	}
	if hasFolderFilter {
		provider, ok := h.knowledges.(integrationFolderProvider)
		if !ok {
			integrationError(c, http.StatusInternalServerError, "list_failed", "failed to list knowledge")
			return
		}
		resolved, resolveErr := provider.ResolveIntegrationFolderIDs(c.Request.Context(), principal.TenantID, []string{kbID}, folderIDs, nil)
		if resolveErr != nil {
			integrationError(c, http.StatusBadRequest, "invalid_folder_ids", "invalid folder ids")
			return
		}
		folderIDs = resolved
	}
	rows, err := h.knowledges.ListKnowledgeByKnowledgeBaseID(c.Request.Context(), kbID)
	if err != nil {
		integrationError(c, http.StatusInternalServerError, "list_failed", "failed to list knowledge")
		return
	}
	tagNames, err := h.service.KnowledgeTagNames(c.Request.Context(), principal, kbID)
	if err != nil {
		integrationError(c, http.StatusInternalServerError, "list_failed", "failed to list knowledge metadata")
		return
	}
	allowedTagIDs := make(map[string]struct{})
	if hasFolderFilter {
		for _, folderID := range folderIDs {
			allowedTagIDs[folderID] = struct{}{}
		}
	}
	if filterDisabledFolders {
		provider, ok := h.knowledges.(integrationSearchableFolderProvider)
		if !ok {
			integrationError(c, http.StatusInternalServerError, "list_failed", "failed to list knowledge")
			return
		}
		searchableTagIDs, searchableErr := provider.SearchableTagIDs(c.Request.Context(), principal.TenantID, kbID)
		if searchableErr != nil {
			integrationError(c, http.StatusInternalServerError, "list_failed", "failed to list knowledge")
			return
		}
		searchable := make(map[string]struct{}, len(searchableTagIDs))
		for _, tagID := range searchableTagIDs {
			searchable[tagID] = struct{}{}
		}
		if len(allowedTagIDs) == 0 {
			allowedTagIDs = searchable
		} else {
			for tagID := range allowedTagIDs {
				if _, ok := searchable[tagID]; !ok {
					delete(allowedTagIDs, tagID)
				}
			}
		}
	}
	result := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		if hasFolderFilter || filterDisabledFolders {
			if _, ok := allowedTagIDs[row.TagID]; !ok {
				continue
			}
		}
		result = append(result, gin.H{
			"id": row.ID, "knowledge_base_id": row.KnowledgeBaseID,
			"title": row.Title, "description": row.Description,
			"file_name": row.FileName, "file_type": row.FileType,
			"tag_id": row.TagID, "tag_name": tagNames[row.TagID], "parse_status": row.ParseStatus,
			"enable_status": row.EnableStatus, "updated_at": row.UpdatedAt,
		})
	}
	h.service.AuditResources(c.Request.Context(), principal, "api.knowledge.list", "allowed", "", []string{kbID})
	integrationData(c, http.StatusOK, result)
}

type integrationTableAnalysisQuery struct {
	ID    string `json:"id"`
	Query string `json:"query"`
}

type integrationTableAnalysisResult struct {
	ID       string              `json:"id"`
	Status   string              `json:"status"`
	SQL      string              `json:"sql,omitempty"`
	Rows     []map[string]string `json:"rows,omitempty"`
	RowCount int                 `json:"row_count"`
	Error    string              `json:"error,omitempty"`
}

// AnalyzeKnowledgeTable generates and executes read-only DuckDB SQL for one
// tabular knowledge item. The model and tool configuration remain owned by
// WeKnora; callers only select an allowed KB/file and provide natural-language
// questions.
func (h *IntegrationHandler) AnalyzeKnowledgeTable(c *gin.Context) {
	if !h.enforceRate(c, "api.table.analyze", 20) {
		return
	}
	if !h.requireScope(c, "table:analyze") {
		return
	}
	h.limitRequestBody(c)
	var req struct {
		KnowledgeBaseID string                          `json:"knowledge_base_id" binding:"required"`
		KnowledgeID     string                          `json:"knowledge_id" binding:"required"`
		Queries         []integrationTableAnalysisQuery `json:"queries" binding:"required"`
		MaxRows         int                             `json:"max_rows"`
	}
	if c.ShouldBindJSON(&req) != nil {
		integrationError(c, http.StatusBadRequest, "invalid_table_analysis", "knowledge_base_id, knowledge_id and queries are required")
		return
	}
	req.KnowledgeBaseID = strings.TrimSpace(req.KnowledgeBaseID)
	req.KnowledgeID = strings.TrimSpace(req.KnowledgeID)
	if req.MaxRows == 0 {
		req.MaxRows = 200
	}
	if req.KnowledgeBaseID == "" || req.KnowledgeID == "" || len(req.Queries) == 0 || len(req.Queries) > 20 || req.MaxRows < 1 || req.MaxRows > 1000 {
		integrationError(c, http.StatusBadRequest, "invalid_table_analysis", "queries must contain 1-20 items and max_rows must be 1-1000")
		return
	}
	for _, query := range req.Queries {
		if strings.TrimSpace(query.ID) == "" || strings.TrimSpace(query.Query) == "" || len(query.Query) > h.limits.maxQueryBytes {
			integrationError(c, http.StatusBadRequest, "invalid_table_analysis", "each query requires a non-empty id and query")
			return
		}
	}
	principal := integrationPrincipal(c)
	if h.service.AuthorizeKnowledgeBases(principal, []string{req.KnowledgeBaseID}) != nil {
		h.service.AuditResources(c.Request.Context(), principal, "api.table.analyze", "denied", "knowledge_base_denied", []string{req.KnowledgeBaseID})
		integrationError(c, http.StatusForbidden, "knowledge_base_denied", "knowledge base access denied")
		return
	}
	knowledge, err := h.knowledges.GetKnowledgeByIDOnly(c.Request.Context(), req.KnowledgeID)
	if err != nil {
		integrationError(c, http.StatusInternalServerError, "knowledge_lookup_failed", "failed to load knowledge")
		return
	}
	if knowledge == nil || knowledge.KnowledgeBaseID != req.KnowledgeBaseID || knowledge.TenantID != principal.TenantID {
		integrationError(c, http.StatusNotFound, "knowledge_not_found", "knowledge was not found in the requested knowledge base")
		return
	}
	fileType := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(knowledge.FileType), "."))
	if fileType != "csv" && fileType != "xlsx" && fileType != "xls" {
		integrationError(c, http.StatusBadRequest, "unsupported_table_type", "only CSV, XLSX and XLS knowledge can be analyzed")
		return
	}
	model, err := h.models.GetDefaultModel(c.Request.Context(), types.ModelTypeKnowledgeQA, "chat")
	if err != nil || model == nil {
		integrationError(c, http.StatusServiceUnavailable, "table_analysis_model_unavailable", "default chat model is unavailable")
		return
	}
	chatModel, err := h.models.GetChatModel(c.Request.Context(), model.ID)
	if err != nil {
		integrationError(c, http.StatusServiceUnavailable, "table_analysis_model_unavailable", "default chat model is unavailable")
		return
	}
	tool := tools.NewDataAnalysisTool(h.kbs, h.knowledges, h.tenant, h.files, h.duckdb, "integration_"+uuid.NewString())
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tool.Cleanup(cleanupCtx)
	}()
	schema, err := tool.LoadFromKnowledge(c.Request.Context(), knowledge)
	if err != nil {
		integrationError(c, http.StatusUnprocessableEntity, "table_load_failed", "tabular knowledge could not be loaded")
		return
	}
	results := make([]integrationTableAnalysisResult, 0, len(req.Queries))
	for _, query := range req.Queries {
		results = append(results, h.runIntegrationTableQuery(c.Request.Context(), chatModel, tool, schema, knowledge.ID, query, req.MaxRows))
	}
	h.service.AuditResources(c.Request.Context(), principal, "api.table.analyze", "allowed", "", []string{req.KnowledgeBaseID})
	integrationData(c, http.StatusOK, gin.H{"knowledge_base_id": req.KnowledgeBaseID, "knowledge_id": req.KnowledgeID, "agent_id": types.BuiltinDataAnalystID, "results": results})
}

func (h *IntegrationHandler) runIntegrationTableQuery(ctx context.Context, model chat.Chat, tool *tools.DataAnalysisTool, schema *tools.TableSchema, knowledgeID string, query integrationTableAnalysisQuery, maxRows int) integrationTableAnalysisResult {
	result := integrationTableAnalysisResult{ID: query.ID, Status: "failed", Rows: []map[string]string{}}
	prompt := fmt.Sprintf(`You are the SQL planning component of WeKnora's built-in data analyst.
Generate exactly one read-only DuckDB SELECT statement that answers the question using the table below.
Use exact quoted column names from the schema. Do not use external files, table functions, PRAGMA, SHOW, DESCRIBE, EXPLAIN, or multiple statements.
Question: %s
Knowledge ID (return this unchanged in knowledge_id): %s
SQL table name (use this exact quoted identifier in FROM): "%s"
Schema:
%s`, strings.TrimSpace(query.Query), knowledgeID, schema.TableName, schema.Description())
	response, err := model.Chat(ctx, []chat.Message{{Role: "user", Content: prompt}}, &chat.ChatOptions{Temperature: 0.1, Format: utils.GenerateSchema[tools.DataAnalysisInput]()})
	if err != nil {
		result.Error = "failed to generate SQL"
		return result
	}
	var input tools.DataAnalysisInput
	if json.Unmarshal([]byte(response.Content), &input) != nil || strings.TrimSpace(input.Sql) == "" {
		result.Error = "model returned invalid SQL"
		return result
	}
	input.KnowledgeID = knowledgeID
	input.MaxRows = maxRows
	payload, _ := json.Marshal(input)
	toolResult, err := tool.Execute(ctx, payload)
	if err != nil || toolResult == nil || !toolResult.Success {
		result.Error = "SQL execution failed"
		return result
	}
	result.Status = "completed"
	if value, ok := toolResult.Data["query"].(string); ok {
		result.SQL = value
	}
	if rows, ok := toolResult.Data["rows"].([]map[string]string); ok {
		result.Rows = rows
	}
	result.RowCount = len(result.Rows)
	return result
}

func (h *IntegrationHandler) Search(c *gin.Context) {
	if !h.enforceRate(c, "api.search", 60) {
		return
	}
	if !h.requireScope(c, "rag:search") {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(h.limits.maxRequestBytes))
	var req struct {
		KnowledgeBaseIDs      *[]string `json:"knowledge_base_ids" binding:"required"`
		KnowledgeIDs          *[]string `json:"knowledge_ids"`
		FolderIDs             *[]string `json:"folder_ids"`
		FilterDisabledFolders bool      `json:"filter_disabled_folders"`
		Query                 string    `json:"query" binding:"required"`
		TopK                  int       `json:"top_k"`
	}
	if c.ShouldBindJSON(&req) != nil || req.KnowledgeBaseIDs == nil || len(*req.KnowledgeBaseIDs) == 0 || strings.TrimSpace(req.Query) == "" {
		h.audit(c, "api.search", "denied", "invalid_request")
		integrationError(c, http.StatusBadRequest, "invalid_search", "knowledge_base_ids and query are required")
		return
	}
	if len(*req.KnowledgeBaseIDs) > h.limits.maxKnowledgeBases || len(req.Query) > h.limits.maxQueryBytes || req.TopK < 0 || req.TopK > h.limits.maxTopK {
		h.audit(c, "api.search", "denied", "limit_exceeded")
		integrationError(c, http.StatusBadRequest, "search_limit_exceeded", "search limits exceeded")
		return
	}
	var knowledgeIDs []string
	if req.KnowledgeIDs != nil {
		if !validIntegrationKnowledgeIDs(*req.KnowledgeIDs, h.limits.maxKnowledgeIDs) {
			h.audit(c, "api.search", "denied", "knowledge_limit_exceeded")
			integrationError(c, http.StatusBadRequest, "invalid_search", "invalid knowledge ids")
			return
		}
		knowledgeIDs = append([]string(nil), *req.KnowledgeIDs...)
	}
	if h.service.AuthorizeKnowledgeBases(integrationPrincipal(c), *req.KnowledgeBaseIDs) != nil {
		h.service.AuditResources(c.Request.Context(), integrationPrincipal(c), "api.search", "denied", "knowledge_base_denied", *req.KnowledgeBaseIDs)
		integrationError(c, http.StatusForbidden, "knowledge_base_denied", "knowledge base access denied")
		return
	}
	var results []*types.SearchResult
	var err error
	if req.FolderIDs == nil || len(*req.FolderIDs) == 0 {
		results, err = h.sessions.SearchKnowledge(c.Request.Context(), *req.KnowledgeBaseIDs, knowledgeIDs, req.FilterDisabledFolders, req.Query)
	} else {
		provider, providerOK := h.knowledges.(integrationFolderProvider)
		searcher, searcherOK := h.sessions.(folderSearchSession)
		if !providerOK || !searcherOK || !validIntegrationKnowledgeIDs(*req.FolderIDs, h.limits.maxKnowledgeIDs) {
			integrationError(c, http.StatusBadRequest, "invalid_folder_ids", "invalid folder ids")
			return
		}
		folderIDs, resolveErr := provider.ResolveIntegrationFolderIDs(c.Request.Context(), integrationPrincipal(c).TenantID, *req.KnowledgeBaseIDs, *req.FolderIDs, knowledgeIDs)
		if resolveErr != nil {
			if resolveErr.Error() == "invalid_knowledge_folder_scope" {
				integrationError(c, http.StatusBadRequest, "invalid_knowledge_folder_scope", "knowledge is outside the folder scope")
				return
			}
			integrationError(c, http.StatusBadRequest, "invalid_folder_ids", "invalid folder ids")
			return
		}
		results, err = searcher.SearchKnowledgeWithFolders(c.Request.Context(), *req.KnowledgeBaseIDs, knowledgeIDs, folderIDs, req.FilterDisabledFolders, req.Query)
	}
	if err != nil {
		if appErr, ok := werrors.IsAppError(err); ok && appErr.Code == werrors.ErrForbidden {
			h.audit(c, "api.search", "denied", "knowledge_denied")
			integrationError(c, http.StatusForbidden, "knowledge_denied", "knowledge access denied")
			return
		}
		integrationError(c, http.StatusInternalServerError, "search_failed", "knowledge search failed")
		return
	}
	limit := h.retrievalResponseLimit(c.Request.Context(), integrationPrincipal(c).TenantID, req.TopK)
	if len(results) > limit {
		results = results[:limit]
	}
	knowledgeBaseNames := make(map[string]string)
	if listed, listErr := h.kbs.ListKnowledgeBases(c.Request.Context()); listErr == nil {
		for _, kb := range listed {
			knowledgeBaseNames[kb.ID] = kb.Name
		}
	}
	publicResults := make([]gin.H, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		publicResults = append(publicResults, gin.H{
			"knowledge_base_id":    result.KnowledgeBaseID,
			"knowledge_base_name":  knowledgeBaseNames[result.KnowledgeBaseID],
			"knowledge_id":         result.KnowledgeID,
			"knowledge_version_id": result.KnowledgeVersionID,
			"chunk_id":             result.ID,
			"title":                result.KnowledgeTitle,
			"source":               result.KnowledgeSource,
			"content":              result.Content,
			"score":                result.Score,
		})
	}
	h.service.AuditResources(c.Request.Context(), integrationPrincipal(c), "api.search", "allowed", "", *req.KnowledgeBaseIDs)
	integrationData(c, http.StatusOK, publicResults)
}

func (h *IntegrationHandler) SearchBatch(c *gin.Context) {
	if !h.enforceRate(c, "api.search.batch", 60) {
		return
	}
	if !h.requireScope(c, "rag:search") {
		return
	}
	h.limitRequestBody(c)
	var req integrationBatchSearchRequest
	if c.ShouldBindJSON(&req) != nil {
		h.audit(c, "api.search.batch", "denied", "invalid_request")
		integrationError(c, http.StatusBadRequest, "invalid_batch_search", "knowledge_base_ids and queries are required")
		return
	}
	if reason := validateIntegrationBatchSearchRequest(req, h.limits); reason != "" {
		h.audit(c, "api.search.batch", "denied", reason)
		integrationError(c, http.StatusBadRequest, "batch_search_limit_exceeded", "batch search limits exceeded")
		return
	}
	knowledgeBaseIDs := *req.KnowledgeBaseIDs
	principal := integrationPrincipal(c)
	if h.service.AuthorizeKnowledgeBases(principal, knowledgeBaseIDs) != nil {
		h.service.AuditResources(c.Request.Context(), principal, "api.search.batch", "denied", "knowledge_base_denied", knowledgeBaseIDs)
		integrationError(c, http.StatusForbidden, "knowledge_base_denied", "knowledge base access denied")
		return
	}
	folderIDsByQuery := make([][]string, len(req.Queries))
	provider, providerOK := h.knowledges.(integrationFolderProvider)
	for i, query := range req.Queries {
		if query.FolderIDs == nil || len(*query.FolderIDs) == 0 {
			continue
		}
		if !providerOK {
			integrationError(c, http.StatusInternalServerError, "search_failed", "knowledge search failed")
			return
		}
		knowledgeIDs := []string(nil)
		if query.KnowledgeIDs != nil {
			knowledgeIDs = *query.KnowledgeIDs
		}
		resolved, resolveErr := provider.ResolveIntegrationFolderIDs(c.Request.Context(), principal.TenantID, knowledgeBaseIDs, *query.FolderIDs, knowledgeIDs)
		if resolveErr != nil {
			if resolveErr.Error() == "invalid_knowledge_folder_scope" {
				integrationError(c, http.StatusBadRequest, "invalid_knowledge_folder_scope", "knowledge is outside the folder scope")
				return
			}
			integrationError(c, http.StatusBadRequest, "invalid_folder_ids", "invalid folder ids")
			return
		}
		folderIDsByQuery[i] = resolved
	}
	batchConfig := types.DefaultRetrievalConfig()
	if h.tenant != nil {
		if tenant, err := h.tenant.GetTenantByID(c.Request.Context(), principal.TenantID); err == nil && tenant != nil {
			batchConfig = types.NormalizeRetrievalConfig(tenant.RetrievalConfig)
		}
	}
	results, forbidden := h.runIntegrationSearchBatch(c.Request.Context(), knowledgeBaseIDs, req.Queries, folderIDsByQuery)
	if forbidden {
		h.service.AuditResources(c.Request.Context(), principal, "api.search.batch", "denied", "knowledge_denied", knowledgeBaseIDs)
		integrationError(c, http.StatusForbidden, "knowledge_denied", "knowledge access denied")
		return
	}
	var budgetStats integrationBatchBudgetStats
	results, budgetStats = applyIntegrationBatchBudget(
		results,
		batchConfig.BatchMaxResults,
		batchConfig.BatchMaxContentChars,
	)
	logger.Infof(c.Request.Context(), "Integration batch response budget applied: before=%d after=%d content_chars=%d affected_queries=%d",
		budgetStats.BeforeResults, budgetStats.AfterResults, budgetStats.ContentChars, budgetStats.AffectedQueries)
	h.service.AuditResources(c.Request.Context(), principal, "api.search.batch", "allowed", "", knowledgeBaseIDs)
	integrationData(c, http.StatusOK, gin.H{"results": results})
}

func validateIntegrationBatchSearchRequest(req integrationBatchSearchRequest, limits integrationLimits) string {
	if req.KnowledgeBaseIDs == nil || len(*req.KnowledgeBaseIDs) == 0 || len(req.Queries) == 0 {
		return "invalid_request"
	}
	if len(*req.KnowledgeBaseIDs) > limits.maxKnowledgeBases || len(req.Queries) > limits.maxBatchQueries {
		return "limit_exceeded"
	}
	seen := make(map[string]struct{}, len(req.Queries))
	for _, query := range req.Queries {
		trimmedID := strings.TrimSpace(query.ID)
		if trimmedID == "" || trimmedID != query.ID || len(query.ID) > 128 || strings.TrimSpace(query.Query) == "" || len(query.Query) > limits.maxQueryBytes || query.TopK < 0 || query.TopK > limits.maxTopK {
			return "limit_exceeded"
		}
		if _, exists := seen[query.ID]; exists {
			return "duplicate_query_id"
		}
		seen[query.ID] = struct{}{}
		if query.KnowledgeIDs != nil && !validIntegrationKnowledgeIDs(*query.KnowledgeIDs, limits.maxKnowledgeIDs) {
			return "knowledge_limit_exceeded"
		}
		if query.FolderIDs != nil && !validIntegrationKnowledgeIDs(*query.FolderIDs, limits.maxKnowledgeIDs) {
			return "folder_limit_exceeded"
		}
	}
	return ""
}

func (h *IntegrationHandler) runIntegrationSearchBatch(ctx context.Context, knowledgeBaseIDs []string, queries []integrationBatchSearchQuery, folderIDsByQuery ...[][]string) ([]integrationBatchSearchResult, bool) {
	tenantID, _ := types.TenantIDFromContext(ctx)
	results := make([]integrationBatchSearchResult, len(queries))
	knowledgeBaseNames := make(map[string]string)
	if listed, err := h.kbs.ListKnowledgeBases(ctx); err == nil {
		for _, kb := range listed {
			knowledgeBaseNames[kb.ID] = kb.Name
		}
	}
	concurrency := h.limits.batchConcurrency
	if concurrency < 1 {
		concurrency = 1
	}
	semaphore := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var forbiddenMu sync.Mutex
	forbidden := false
	for index, query := range queries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			knowledgeIDs := []string(nil)
			if query.KnowledgeIDs != nil {
				knowledgeIDs = append(knowledgeIDs, (*query.KnowledgeIDs)...)
			}
			var found []*types.SearchResult
			var err error
			if len(folderIDsByQuery) > 0 && folderIDsByQuery[0][index] != nil {
				searcher, ok := h.sessions.(folderSearchSession)
				if !ok {
					err = errors.New("folder search is unavailable")
				} else {
					found, err = searcher.SearchKnowledgeWithFolders(ctx, knowledgeBaseIDs, knowledgeIDs, folderIDsByQuery[0][index], query.FilterDisabledFolders, query.Query)
				}
			} else {
				found, err = h.sessions.SearchKnowledge(ctx, knowledgeBaseIDs, knowledgeIDs, query.FilterDisabledFolders, query.Query)
			}
			if err != nil {
				if appErr, ok := werrors.IsAppError(err); ok && appErr.Code == werrors.ErrForbidden {
					forbiddenMu.Lock()
					forbidden = true
					forbiddenMu.Unlock()
				}
				results[index] = integrationBatchSearchResult{ID: query.ID, Status: "failed", Results: []gin.H{}, Error: "search_failed"}
				return
			}
			limit := h.retrievalResponseLimit(ctx, tenantID, query.TopK)
			results[index] = integrationBatchSearchResult{ID: query.ID, Status: "completed", Results: integrationPublicSearchResults(found, knowledgeBaseNames, limit)}
		}()
	}
	wg.Wait()
	return results, forbidden
}

func integrationPublicSearchResults(results []*types.SearchResult, knowledgeBaseNames map[string]string, limit int) []gin.H {
	if len(results) > limit {
		results = results[:limit]
	}
	publicResults := make([]gin.H, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		publicResults = append(publicResults, gin.H{
			"knowledge_base_id": result.KnowledgeBaseID, "knowledge_base_name": knowledgeBaseNames[result.KnowledgeBaseID],
			"knowledge_id": result.KnowledgeID, "knowledge_version_id": result.KnowledgeVersionID,
			"chunk_id": result.ID, "title": result.KnowledgeTitle, "source": result.KnowledgeSource,
			"content": result.Content, "score": result.Score,
		})
	}
	return publicResults
}

type integrationBatchBudgetStats struct {
	BeforeResults   int
	AfterResults    int
	ContentChars    int
	AffectedQueries int
}

func applyIntegrationBatchBudget(results []integrationBatchSearchResult, maxResults, maxContentChars int) ([]integrationBatchSearchResult, integrationBatchBudgetStats) {
	if maxResults <= 0 {
		maxResults = types.DefaultBatchMaxResults
	}
	if maxContentChars <= 0 {
		maxContentChars = types.DefaultBatchMaxContentChars
	}

	trimmed := make([]integrationBatchSearchResult, len(results))
	positions := make([]int, len(results))
	stats := integrationBatchBudgetStats{}
	for i, result := range results {
		stats.BeforeResults += len(result.Results)
		trimmed[i] = result
		trimmed[i].Results = make([]gin.H, 0, len(result.Results))
	}

	seen := make(map[string]struct{})
	for stats.AfterResults < maxResults {
		selectedInRound := false
		remainingCandidates := false
		for i := range results {
			for positions[i] < len(results[i].Results) {
				remainingCandidates = true
				candidate := results[i].Results[positions[i]]
				positions[i]++
				identity := integrationBatchResultIdentity(candidate)
				if identity != "" {
					if _, duplicate := seen[identity]; duplicate {
						continue
					}
				}
				content, _ := candidate["content"].(string)
				contentChars := utf8.RuneCountInString(content)
				if stats.ContentChars+contentChars > maxContentChars {
					continue
				}
				trimmed[i].Results = append(trimmed[i].Results, candidate)
				stats.AfterResults++
				stats.ContentChars += contentChars
				if identity != "" {
					seen[identity] = struct{}{}
				}
				selectedInRound = true
				break
			}
			if stats.AfterResults >= maxResults {
				break
			}
		}
		if !selectedInRound || !remainingCandidates {
			break
		}
	}

	for i := range results {
		if len(trimmed[i].Results) != len(results[i].Results) {
			stats.AffectedQueries++
		}
	}
	return trimmed, stats
}

func integrationBatchResultIdentity(result gin.H) string {
	chunkID := strings.TrimSpace(fmt.Sprint(result["chunk_id"]))
	if chunkID == "" || chunkID == "<nil>" {
		return ""
	}
	versionID := strings.TrimSpace(fmt.Sprint(result["knowledge_version_id"]))
	if versionID != "" && versionID != "<nil>" {
		return versionID + "\x00" + chunkID
	}
	knowledgeID := strings.TrimSpace(fmt.Sprint(result["knowledge_id"]))
	return knowledgeID + "\x00" + chunkID
}

func (h *IntegrationHandler) CreateChatSession(c *gin.Context) {
	if !h.enforceRate(c, "api.chat.session.create", 60) {
		return
	}
	if !h.requireScope(c, "chat:write") {
		return
	}
	principal := integrationPrincipal(c)
	h.limitRequestBody(c)
	var req struct {
		Title             string    `json:"title"`
		KnowledgeBaseMode string    `json:"knowledge_base_mode" binding:"required"`
		KnowledgeBaseIDs  *[]string `json:"knowledge_base_ids"`
	}
	if c.ShouldBindJSON(&req) != nil {
		integrationError(c, http.StatusBadRequest, "invalid_session", "knowledge_base_mode is required")
		return
	}
	requested := []string(nil)
	if req.KnowledgeBaseIDs != nil {
		requested = *req.KnowledgeBaseIDs
	}
	if (req.KnowledgeBaseMode == "selected" && len(requested) == 0) ||
		(req.KnowledgeBaseMode == "all-allowed" && req.KnowledgeBaseIDs != nil) ||
		(req.KnowledgeBaseMode != "selected" && req.KnowledgeBaseMode != "all-allowed") {
		integrationError(c, http.StatusBadRequest, "invalid_knowledge_base_mode", "mode and knowledge_base_ids conflict")
		return
	}
	payload, _ := json.Marshal(req)
	sessionID := uuid.NewString()
	resourceID, replay, err := h.service.ClaimIdempotency(c.Request.Context(), principal, c.FullPath(), c.GetHeader("Idempotency-Key"), string(payload), sessionID)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, integrationauth.ErrConflict) {
			status = http.StatusConflict
		}
		integrationError(c, status, "idempotency_conflict", "invalid or conflicting Idempotency-Key")
		return
	}
	if replay {
		existing, getErr := h.sessions.GetSession(c.Request.Context(), resourceID)
		if getErr != nil {
			integrationError(c, http.StatusConflict, "idempotency_incomplete", "original request is incomplete")
			return
		}
		binding, bindingErr := h.service.GetChatBinding(c.Request.Context(), principal, resourceID)
		if bindingErr != nil {
			integrationError(c, http.StatusConflict, "idempotency_incomplete", "original request is incomplete")
			return
		}
		h.audit(c, "api.chat.session.create", "allowed", "idempotent_replay")
		integrationData(c, http.StatusOK, integrationChatSessionResponse(existing, binding.KnowledgeBaseMode, binding.AllowedKnowledgeBaseIDs()))
		return
	}
	session, err := h.sessions.CreateSession(c.Request.Context(), &types.Session{ID: sessionID, TenantID: principal.TenantID, Title: req.Title})
	if err != nil {
		h.service.ReleaseIdempotency(c.Request.Context(), principal, c.FullPath(), c.GetHeader("Idempotency-Key"))
		integrationError(c, http.StatusInternalServerError, "session_create_failed", "failed to create chat session")
		return
	}
	if err = h.service.CreateChatBinding(c.Request.Context(), principal, session.ID, req.KnowledgeBaseMode, requested); err != nil {
		_ = h.sessions.DeleteSession(c.Request.Context(), session.ID)
		h.service.ReleaseIdempotency(c.Request.Context(), principal, c.FullPath(), c.GetHeader("Idempotency-Key"))
		integrationError(c, http.StatusForbidden, "knowledge_base_denied", "knowledge base access denied")
		return
	}
	allowed := requested
	if req.KnowledgeBaseMode == "all-allowed" {
		allowed = principal.KnowledgeBaseIDs
	}
	h.audit(c, "api.chat.session.create", "allowed", "")
	integrationData(c, http.StatusCreated, integrationChatSessionResponse(session, req.KnowledgeBaseMode, allowed))
}

func integrationChatSessionResponse(session *types.Session, mode string, allowed []string) gin.H {
	return gin.H{"id": session.ID, "title": session.Title, "knowledge_base_mode": mode, "allowed_knowledge_base_ids": allowed}
}

func (h *IntegrationHandler) ListChatSessions(c *gin.Context) {
	if !h.requireScope(c, "chat:read") {
		return
	}
	bindings, err := h.service.ListChatBindings(c.Request.Context(), integrationPrincipal(c))
	if err != nil {
		integrationError(c, http.StatusInternalServerError, "session_list_failed", "failed to list chat sessions")
		return
	}
	sessions := make([]gin.H, 0, len(bindings))
	for _, binding := range bindings {
		session, getErr := h.sessions.GetSession(c.Request.Context(), binding.SessionID)
		if getErr == nil {
			sessions = append(sessions, gin.H{
				"id":                         session.ID,
				"title":                      session.Title,
				"created_at":                 session.CreatedAt,
				"updated_at":                 session.UpdatedAt,
				"knowledge_base_mode":        binding.KnowledgeBaseMode,
				"allowed_knowledge_base_ids": binding.AllowedKnowledgeBaseIDs(),
			})
		}
	}
	integrationData(c, http.StatusOK, sessions)
}

func (h *IntegrationHandler) ListFrequentQuestions(c *gin.Context) {
	if !h.requireScope(c, "chat:read") {
		return
	}
	principal := integrationPrincipal(c)
	questions, err := h.messages.GetFrequentlyAskedQuestions(c.Request.Context(), principal.ClientID, principal.UserID, 3)
	if err != nil {
		integrationError(c, http.StatusInternalServerError, "frequent_questions_failed", "failed to list frequent questions")
		return
	}
	integrationData(c, http.StatusOK, gin.H{"questions": questions})
}

func (h *IntegrationHandler) GetChatSession(c *gin.Context) {
	if !h.requireScope(c, "chat:read") {
		return
	}
	binding, err := h.service.GetChatBinding(c.Request.Context(), integrationPrincipal(c), c.Param("session_id"))
	if err != nil {
		integrationError(c, http.StatusForbidden, "session_denied", "session access denied")
		return
	}
	session, err := h.sessions.GetSession(c.Request.Context(), c.Param("session_id"))
	if err != nil {
		integrationError(c, http.StatusNotFound, "session_not_found", "session not found")
		return
	}
	integrationData(c, http.StatusOK, gin.H{
		"id":                         session.ID,
		"title":                      session.Title,
		"created_at":                 session.CreatedAt,
		"updated_at":                 session.UpdatedAt,
		"knowledge_base_mode":        binding.KnowledgeBaseMode,
		"allowed_knowledge_base_ids": binding.AllowedKnowledgeBaseIDs(),
	})
}

func (h *IntegrationHandler) UpdateChatSession(c *gin.Context) {
	if !h.requireScope(c, "chat:write") {
		return
	}
	if _, err := h.service.GetChatBinding(c.Request.Context(), integrationPrincipal(c), c.Param("session_id")); err != nil {
		integrationError(c, http.StatusForbidden, "session_denied", "session access denied")
		return
	}
	var req struct {
		Title string `json:"title" binding:"required"`
	}
	if c.ShouldBindJSON(&req) != nil {
		integrationError(c, http.StatusBadRequest, "invalid_session_title", "title is required")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" || len([]rune(req.Title)) > 100 {
		integrationError(c, http.StatusBadRequest, "invalid_session_title", "title must be between 1 and 100 characters")
		return
	}
	session, err := h.sessions.GetSession(c.Request.Context(), c.Param("session_id"))
	if err != nil {
		integrationError(c, http.StatusNotFound, "session_not_found", "session not found")
		return
	}
	session.Title = req.Title
	if err = h.sessions.UpdateSession(c.Request.Context(), session); err != nil {
		integrationError(c, http.StatusInternalServerError, "session_update_failed", "failed to update chat session")
		return
	}
	h.audit(c, "api.chat.session.update", "allowed", "")
	integrationData(c, http.StatusOK, gin.H{"id": session.ID, "title": session.Title})
}

func (h *IntegrationHandler) DeleteChatSession(c *gin.Context) {
	if !h.requireScope(c, "chat:write") {
		return
	}
	principal := integrationPrincipal(c)
	if _, err := h.service.GetChatBinding(c.Request.Context(), principal, c.Param("session_id")); err != nil {
		integrationError(c, http.StatusForbidden, "session_denied", "session access denied")
		return
	}
	if err := h.sessions.DeleteSession(c.Request.Context(), c.Param("session_id")); err != nil {
		integrationError(c, http.StatusInternalServerError, "session_delete_failed", "failed to delete chat session")
		return
	}
	if err := h.service.DeleteChatBinding(c.Request.Context(), principal, c.Param("session_id")); err != nil {
		integrationError(c, http.StatusInternalServerError, "session_binding_delete_failed", "chat session was deleted but its integration binding could not be removed")
		return
	}
	h.audit(c, "api.chat.session.delete", "allowed", "")
	c.Status(http.StatusNoContent)
}

func (h *IntegrationHandler) ListChatMessages(c *gin.Context) {
	if !h.requireScope(c, "chat:read") {
		return
	}
	binding, err := h.service.GetChatBinding(c.Request.Context(), integrationPrincipal(c), c.Param("session_id"))
	if err != nil {
		integrationError(c, http.StatusForbidden, "session_denied", "session access denied")
		return
	}
	messages, err := h.messages.GetMessagesBySession(c.Request.Context(), binding.SessionID, 1, 100)
	if err != nil {
		integrationError(c, http.StatusInternalServerError, "message_list_failed", "failed to list messages")
		return
	}
	integrationData(c, http.StatusOK, messages)
}

func (h *IntegrationHandler) SynthesizeVoice(c *gin.Context) {
	if !h.requireScope(c, "chat:read") {
		return
	}
	binding, err := h.service.GetChatBinding(c.Request.Context(), integrationPrincipal(c), c.Param("session_id"))
	if err != nil {
		integrationError(c, http.StatusForbidden, "session_denied", "session access denied")
		return
	}
	var request session.VoiceSynthesisRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		integrationError(c, http.StatusBadRequest, "invalid_request", "invalid voice synthesis request")
		return
	}
	message, err := h.messages.GetMessage(c.Request.Context(), binding.SessionID, request.MessageID)
	if err != nil || message == nil || message.Role != "assistant" || !message.IsCompleted {
		integrationError(c, http.StatusNotFound, "message_not_found", "completed assistant message not found")
		return
	}
	session.SynthesizeVoiceMessage(c, h.models, message, request)
}

func (h *IntegrationHandler) TranscribeVoice(c *gin.Context) {
	if !h.requireScope(c, "chat:write") {
		return
	}
	if _, err := h.service.GetChatBinding(c.Request.Context(), integrationPrincipal(c), c.Param("session_id")); err != nil {
		integrationError(c, http.StatusForbidden, "session_denied", "session access denied")
		return
	}
	modelID := strings.TrimSpace(c.PostForm("model_id"))
	if modelID == "" {
		integrationError(c, http.StatusBadRequest, "invalid_request", "model_id is required")
		return
	}
	header, err := c.FormFile("audio")
	if err != nil || header == nil || header.Size <= 0 || header.Size > 25<<20 {
		integrationError(c, http.StatusBadRequest, "invalid_audio", "audio file must be between 1 byte and 25 MiB")
		return
	}
	file, err := header.Open()
	if err != nil {
		integrationError(c, http.StatusBadRequest, "invalid_audio", "audio file cannot be opened")
		return
	}
	defer file.Close()
	audio, err := io.ReadAll(io.LimitReader(file, (25<<20)+1))
	if err != nil || len(audio) > 25<<20 {
		integrationError(c, http.StatusBadRequest, "invalid_audio", "audio file is too large")
		return
	}
	model, err := h.models.GetASRModel(c.Request.Context(), modelID)
	if err != nil {
		integrationError(c, http.StatusBadRequest, "invalid_model", "asr model is unavailable")
		return
	}
	result, err := model.Transcribe(c.Request.Context(), audio, header.Filename)
	if err != nil {
		integrationError(c, http.StatusInternalServerError, "transcription_failed", "audio transcription failed")
		return
	}
	integrationData(c, http.StatusOK, gin.H{"text": result.Text, "segments": result.Segments})
}

func (h *IntegrationHandler) SendChatMessage(c *gin.Context) {
	if !h.enforceRate(c, "api.chat.message.create", 60) {
		return
	}
	if !h.requireScope(c, "chat:write") {
		return
	}
	principal := integrationPrincipal(c)
	h.limitRequestBody(c)
	binding, err := h.service.GetChatBinding(c.Request.Context(), principal, c.Param("session_id"))
	if err != nil {
		integrationError(c, http.StatusForbidden, "session_denied", "session access denied")
		return
	}
	var req struct {
		Query                    string    `json:"query" binding:"required"`
		AgentID                  string    `json:"agent_id"`
		SelectedKnowledgeBaseIDs *[]string `json:"selected_knowledge_base_ids"`
		FilterDisabledFolders    bool      `json:"filter_disabled_folders"`
		Images                   []struct {
			Data string `json:"data"`
		} `json:"images"`
		AttachmentUploads []struct {
			Data     string `json:"data"`
			FileName string `json:"file_name"`
			FileSize int64  `json:"file_size"`
		} `json:"attachment_uploads"`
		VoiceMetadata map[string]string `json:"voice_metadata"`
	}
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.Query) == "" || len(req.Query) > h.limits.maxQueryBytes {
		integrationError(c, http.StatusBadRequest, "invalid_message", "query is required")
		return
	}
	if len(req.Images) > 5 || len(req.AttachmentUploads) > 5 {
		integrationError(c, http.StatusBadRequest, "invalid_attachment", "at most 5 images and 5 files are allowed")
		return
	}
	var customAgent *types.CustomAgent
	if req.AgentID != "" {
		if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`).MatchString(req.AgentID) {
			integrationError(c, http.StatusBadRequest, "invalid_agent", "agent_id is invalid")
			return
		}
		customAgent, err = h.agents.GetAgentByID(c.Request.Context(), req.AgentID)
		if err != nil || customAgent == nil {
			integrationError(c, http.StatusForbidden, "agent_denied", "agent access denied")
			return
		}
	}
	agentMode := customAgent != nil && customAgent.IsAgentMode()
	imageURLs := make([]string, 0, len(req.Images))
	messageImages := make(types.MessageImages, 0, len(req.Images))
	for _, image := range req.Images {
		if !isSupportedIntegrationImage(image.Data) {
			integrationError(c, http.StatusBadRequest, "invalid_image", "image must be a supported base64 data URL")
			return
		}
		imageURLs = append(imageURLs, image.Data)
		messageImages = append(messageImages, types.MessageImage{URL: image.Data})
	}
	processedAttachments := make(types.MessageAttachments, 0, len(req.AttachmentUploads))
	for _, upload := range req.AttachmentUploads {
		decoded, decodeErr := base64.StdEncoding.DecodeString(upload.Data)
		if decodeErr != nil || int64(len(decoded)) != upload.FileSize || upload.FileSize <= 0 || upload.FileSize > 20*1024*1024 {
			integrationError(c, http.StatusBadRequest, "invalid_attachment", "file payload or size is invalid")
			return
		}
		processed, processErr := h.attachments.ProcessAttachment(c.Request.Context(), decoded, upload.FileName, upload.FileSize, principal.TenantID, "")
		if processErr != nil {
			integrationError(c, http.StatusBadRequest, "invalid_attachment", processErr.Error())
			return
		}
		processedAttachments = append(processedAttachments, *processed)
	}
	voiceMetadata, marshalErr := json.Marshal(req.VoiceMetadata)
	if marshalErr != nil {
		integrationError(c, http.StatusBadRequest, "invalid_voice_metadata", "voice metadata is invalid")
		return
	}
	selected, err := h.service.ResolveMessageKnowledgeBases(principal, binding, req.SelectedKnowledgeBaseIDs)
	if err != nil {
		if errors.Is(err, integrationauth.ErrInvalid) {
			integrationError(c, http.StatusBadRequest, "invalid_knowledge_base_selection", "message selection conflicts with session mode")
			return
		}
		integrationError(c, http.StatusForbidden, "knowledge_base_denied", "knowledge base selection denied")
		return
	}
	payload, _ := json.Marshal(req)
	assistantID := uuid.NewString()
	resourceID, replay, err := h.service.ClaimIdempotency(c.Request.Context(), principal, c.FullPath(), c.GetHeader("Idempotency-Key"), string(payload), assistantID)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, integrationauth.ErrConflict) {
			status = http.StatusConflict
		}
		integrationError(c, status, "idempotency_conflict", "invalid or conflicting Idempotency-Key")
		return
	}
	if replay {
		if _, getErr := h.messages.GetMessage(c.Request.Context(), binding.SessionID, resourceID); getErr != nil {
			integrationError(c, http.StatusConflict, "idempotency_incomplete", "original request is incomplete")
			return
		}
		h.writeStoredEvents(c, binding, resourceID, c.GetHeader("Last-Event-ID"))
		return
	}
	requestID := uuid.NewString()
	userMessage, err := h.messages.CreateMessage(c.Request.Context(), &types.Message{ID: uuid.NewString(), SessionID: binding.SessionID, RequestID: requestID, Content: req.Query, Role: "user", IsCompleted: true, Channel: "integration", Images: messageImages, Attachments: processedAttachments, VoiceMetadata: types.JSON(voiceMetadata)})
	if err != nil {
		h.service.ReleaseIdempotency(c.Request.Context(), principal, c.FullPath(), c.GetHeader("Idempotency-Key"))
		integrationError(c, http.StatusInternalServerError, "message_create_failed", "failed to create user message")
		return
	}
	if session, getErr := h.sessions.GetSession(c.Request.Context(), binding.SessionID); getErr == nil && strings.TrimSpace(session.Title) == "" {
		title := []rune(strings.TrimSpace(req.Query))
		if len(title) > 30 {
			title = title[:30]
		}
		session.Title = string(title)
		_ = h.sessions.UpdateSession(c.Request.Context(), session)
	}
	assistantMessage, err := h.messages.CreateMessage(c.Request.Context(), &types.Message{ID: assistantID, SessionID: binding.SessionID, RequestID: requestID, Role: "assistant", Channel: "integration"})
	if err != nil {
		_ = h.messages.DeleteMessage(c.Request.Context(), binding.SessionID, userMessage.ID)
		h.service.ReleaseIdempotency(c.Request.Context(), principal, c.FullPath(), c.GetHeader("Idempotency-Key"))
		integrationError(c, http.StatusInternalServerError, "message_create_failed", "failed to create assistant message")
		return
	}
	aguiEnabled := customAgent != nil && customAgent.Config.AGUIEnabled
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Accel-Buffering", "no")
	var streamMu sync.Mutex
	appendAndWrite := func(ctx context.Context, eventName string, data any) error {
		streamMu.Lock()
		defer streamMu.Unlock()
		stored, appendErr := h.service.AppendStreamEvent(ctx, binding.SessionID, assistantMessage.ID, eventName, data)
		if appendErr != nil {
			return appendErr
		}
		return writeIntegrationSSEEvent(c, stored)
	}
	executionMode := "quick-answer"
	if agentMode {
		executionMode = "smart-reasoning"
	}
	appendAndWrite(c.Request.Context(), "message.created", gin.H{"user_message_id": userMessage.ID, "selected_knowledge_base_ids": selected, "agui_enabled": aguiEnabled, "execution_mode": executionMode})
	eventBus := event.NewEventBus()
	var answer strings.Builder
	var answerStream integrationUTF8Stream
	var thinkingStream integrationUTF8Stream
	var reflectionStream integrationUTF8Stream
	var references types.References = types.References{}
	retrievalCallID := "knowledge-retrieval-" + assistantMessage.ID
	retrievalStartedAt := time.Now()
	generationDone := make(chan error, 1)
	var generationDoneOnce sync.Once
	eventBus.On(event.EventAgentFinalAnswer, func(ctx context.Context, evt event.Event) error {
		data, ok := evt.Data.(event.AgentFinalAnswerData)
		if !ok {
			return nil
		}
		if data.Content != "" {
			answer.WriteString(data.Content)
			if content := answerStream.Push(data.Content); content != "" {
				appendAndWrite(ctx, "answer.delta", gin.H{"content": content})
			}
		}
		if data.Done {
			generationDoneOnce.Do(func() { generationDone <- nil })
		}
		return nil
	})
	eventBus.On(event.EventError, func(_ context.Context, _ event.Event) error {
		generationDoneOnce.Do(func() { generationDone <- errors.New("generation stream failed") })
		return nil
	})
	eventBus.On(event.EventAgentReferences, func(ctx context.Context, evt event.Event) error {
		if data, ok := evt.Data.(event.AgentReferencesData); ok {
			if values, ok := integrationReferences(data.References); ok {
				references = values
				if aguiEnabled && !agentMode {
					appendAndWrite(ctx, "tool_result", event.AgentToolResultData{ToolCallID: retrievalCallID, ToolName: "knowledge_base_search", Output: fmt.Sprintf("检索完成，找到 %d 条相关资料", len(values)), Success: true, Duration: time.Since(retrievalStartedAt).Milliseconds()})
				}
			}
		}
		return nil
	})
	eventBus.On(event.EventAgentThought, func(ctx context.Context, evt event.Event) error {
		if !aguiEnabled {
			return nil
		}
		if data, ok := evt.Data.(event.AgentThoughtData); ok {
			content := thinkingStream.Push(data.Content)
			appendAndWrite(ctx, "thinking", gin.H{"content": content, "iteration": data.Iteration, "done": data.Done, "event_id": evt.ID})
		}
		return nil
	})
	eventBus.On(event.EventAgentToolCall, func(ctx context.Context, evt event.Event) error {
		if !aguiEnabled {
			return nil
		}
		if data, ok := evt.Data.(event.AgentToolCallData); ok {
			appendAndWrite(ctx, "tool_call", data)
		}
		return nil
	})
	eventBus.On(event.EventAgentToolResult, func(ctx context.Context, evt event.Event) error {
		if !aguiEnabled {
			return nil
		}
		if data, ok := evt.Data.(event.AgentToolResultData); ok {
			appendAndWrite(ctx, "tool_result", data)
		}
		return nil
	})
	eventBus.On(event.EventAgentReflection, func(ctx context.Context, evt event.Event) error {
		if !aguiEnabled {
			return nil
		}
		if data, ok := evt.Data.(event.AgentReflectionData); ok {
			data.Content = reflectionStream.Push(data.Content)
			appendAndWrite(ctx, "reflection", data)
		}
		return nil
	})
	generationCtx, cancelGeneration := newIntegrationGenerationContext(c.Request.Context())
	generationKey := binding.SessionID + ":" + assistantMessage.ID
	h.generations.Store(generationKey, cancelGeneration)
	defer func() {
		cancelGeneration()
		h.generations.Delete(generationKey)
	}()
	session, err := h.sessions.GetSession(generationCtx, binding.SessionID)
	if err == nil {
		qaRequest := &types.QARequest{Session: session, Query: req.Query, AssistantMessageID: assistantMessage.ID, KnowledgeBaseIDs: selected, UserMessageID: userMessage.ID, ImageURLs: imageURLs, Attachments: processedAttachments, CustomAgent: customAgent, FilterDisabledFolders: req.FilterDisabledFolders}
		if agentMode {
			err = h.sessions.AgentQA(generationCtx, qaRequest, eventBus)
		} else {
			if aguiEnabled {
				appendAndWrite(generationCtx, "tool_call", event.AgentToolCallData{ToolCallID: retrievalCallID, ToolName: "knowledge_base_search", Arguments: map[string]any{"query": req.Query, "knowledge_base_count": len(selected)}, Hint: "正在检索授权知识库"})
			}
			err = h.sessions.KnowledgeQA(generationCtx, qaRequest, eventBus)
		}
	}
	if err == nil {
		select {
		case err = <-generationDone:
		case <-generationCtx.Done():
			err = generationCtx.Err()
		}
	}
	if generationCtx.Err() != nil {
		return
	}
	if err != nil {
		appendAndWrite(generationCtx, "error", gin.H{"code": "generation_failed", "retryable": false, "status": "failed"})
		return
	}
	assistantMessage.Content = answer.String()
	assistantMessage.IsCompleted = true
	assistantMessage.KnowledgeReferences = references
	if err = h.messages.UpdateMessage(generationCtx, assistantMessage); err != nil {
		appendAndWrite(generationCtx, "error", gin.H{"code": "message_persist_failed", "retryable": true, "status": "failed"})
		return
	}
	if appendErr := appendAndWrite(generationCtx, "answer.completed", gin.H{"status": "completed", "answer": assistantMessage.Content, "selected_knowledge_base_ids": selected, "references": references}); appendErr != nil {
		logger.Errorf(generationCtx, "Failed to append integration completion event: %v", appendErr)
		writeIntegrationSSEError(c, binding.SessionID, assistantMessage.ID, "stream_event_persist_failed")
		return
	}
	h.service.AuditResources(generationCtx, principal, "api.chat.message.create", "allowed", "", selected)
}

func newIntegrationGenerationContext(requestContext context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(context.WithoutCancel(requestContext))
}

func integrationReferences(value any) (types.References, bool) {
	switch references := value.(type) {
	case types.References:
		return references, true
	case []*types.SearchResult:
		return types.References(references), true
	case []interface{}:
		result := make(types.References, 0, len(references))
		for _, reference := range references {
			searchResult, ok := reference.(*types.SearchResult)
			if !ok {
				return nil, false
			}
			result = append(result, searchResult)
		}
		return result, true
	default:
		return nil, false
	}
}

func isSupportedIntegrationImage(data string) bool {
	for _, prefix := range []string{"data:image/jpeg;base64,", "data:image/png;base64,", "data:image/gif;base64,", "data:image/webp;base64,"} {
		if strings.HasPrefix(data, prefix) {
			decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(data, prefix))
			return err == nil && len(decoded) > 0 && len(decoded) <= 10*1024*1024
		}
	}
	return false
}

func (h *IntegrationHandler) writeStoredEvents(c *gin.Context, binding *integrationauth.ChatBinding, messageID, after string) {
	events, gone, err := h.service.ListStreamEvents(c.Request.Context(), binding, messageID, after)
	if err != nil {
		integrationError(c, http.StatusInternalServerError, "event_read_failed", "failed to read message events")
		return
	}
	if gone {
		c.JSON(http.StatusGone, gin.H{"success": false, "error": gin.H{"code": "cursor_expired", "message_snapshot_url": "/api/integration/v1/chat/sessions/" + binding.SessionID + "/messages/" + messageID}})
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Accel-Buffering", "no")
	for _, stored := range events {
		writeIntegrationSSEEvent(c, &stored)
	}
}

func writeIntegrationSSEEvent(c *gin.Context, stored *integrationauth.StreamEvent) error {
	envelope := gin.H{"event_id": stored.EventID, "event": stored.Event, "session_id": stored.SessionID, "message_id": stored.MessageID, "sequence": stored.Sequence, "occurred_at": stored.OccurredAt, "data": json.RawMessage(stored.DataJSON)}
	data, _ := json.Marshal(envelope)
	_, err := c.Writer.Write([]byte("id: " + stored.EventID + "\nevent: " + stored.Event + "\ndata: " + string(data) + "\n\n"))
	c.Writer.Flush()
	return err
}

func writeIntegrationSSEError(c *gin.Context, sessionID, messageID, code string) {
	eventID := uuid.NewString()
	envelope := gin.H{
		"event_id":    eventID,
		"event":       "error",
		"session_id":  sessionID,
		"message_id":  messageID,
		"occurred_at": time.Now(),
		"data": gin.H{
			"code":      code,
			"retryable": true,
			"status":    "failed",
		},
	}
	data, _ := json.Marshal(envelope)
	_, _ = c.Writer.Write([]byte("id: " + eventID + "\nevent: error\ndata: " + string(data) + "\n\n"))
	c.Writer.Flush()
}

type integrationUTF8Stream struct {
	pending []byte
}

func (s *integrationUTF8Stream) Push(chunk string) string {
	data := append(s.pending, []byte(chunk)...)
	validEnd := 0
	for validEnd < len(data) && utf8.FullRune(data[validEnd:]) {
		_, size := utf8.DecodeRune(data[validEnd:])
		validEnd += size
	}
	content := string(data[:validEnd])
	s.pending = append(s.pending[:0], data[validEnd:]...)
	return content
}

func (h *IntegrationHandler) GetMessageSnapshot(c *gin.Context) {
	if !h.requireScope(c, "chat:read") {
		return
	}
	binding, err := h.service.GetChatBinding(c.Request.Context(), integrationPrincipal(c), c.Param("session_id"))
	if err != nil {
		integrationError(c, http.StatusForbidden, "session_denied", "session access denied")
		return
	}
	message, err := h.messages.GetMessage(c.Request.Context(), binding.SessionID, c.Param("message_id"))
	if err != nil {
		integrationError(c, http.StatusNotFound, "message_not_found", "message not found")
		return
	}
	status := "running"
	var references any = []any{}
	selectedKnowledgeBaseIDs := []string{}
	lastEventID := ""
	events, _, _ := h.service.ListStreamEvents(c.Request.Context(), binding, message.ID, "")
	status = integrationMessageStatus(message.IsCompleted, events)
	for _, stored := range events {
		lastEventID = stored.EventID
		if stored.Event == "answer.completed" {
			var data struct {
				References               any      `json:"references"`
				SelectedKnowledgeBaseIDs []string `json:"selected_knowledge_base_ids"`
			}
			if json.Unmarshal([]byte(stored.DataJSON), &data) == nil {
				references = data.References
				selectedKnowledgeBaseIDs = data.SelectedKnowledgeBaseIDs
			}
		}
	}
	integrationData(c, http.StatusOK, gin.H{"message": message, "status": status, "references": references, "selected_knowledge_base_ids": selectedKnowledgeBaseIDs, "last_event_id": lastEventID})
}

func integrationMessageStatus(completed bool, events []integrationauth.StreamEvent) string {
	status := "running"
	if completed {
		status = "completed"
	}
	for _, stored := range events {
		if stored.Event != "error" {
			continue
		}
		var data struct {
			Status string `json:"status"`
		}
		if json.Unmarshal([]byte(stored.DataJSON), &data) == nil && data.Status != "" {
			status = data.Status
		}
	}
	return status
}

func (h *IntegrationHandler) GetMessageEvents(c *gin.Context) {
	if !h.requireScope(c, "chat:read") {
		return
	}
	binding, err := h.service.GetChatBinding(c.Request.Context(), integrationPrincipal(c), c.Param("session_id"))
	if err != nil {
		integrationError(c, http.StatusForbidden, "session_denied", "session access denied")
		return
	}
	after := c.Query("after_event_id")
	if after == "" {
		after = c.GetHeader("Last-Event-ID")
	}
	h.writeStoredEvents(c, binding, c.Param("message_id"), after)
}

func (h *IntegrationHandler) CancelMessage(c *gin.Context) {
	if !h.enforceRate(c, "api.chat.message.cancel", 120) {
		return
	}
	if !h.requireScope(c, "chat:write") {
		return
	}
	binding, err := h.service.GetChatBinding(c.Request.Context(), integrationPrincipal(c), c.Param("session_id"))
	if err != nil {
		integrationError(c, http.StatusForbidden, "session_denied", "session access denied")
		return
	}
	messageID := c.Param("message_id")
	message, getErr := h.messages.GetMessage(c.Request.Context(), binding.SessionID, messageID)
	if getErr != nil {
		integrationError(c, http.StatusNotFound, "message_not_found", "message not found")
		return
	}
	if message.IsCompleted {
		events, _, _ := h.service.ListStreamEvents(c.Request.Context(), binding, message.ID, "")
		integrationData(c, http.StatusOK, gin.H{"status": integrationMessageStatus(true, events)})
		return
	}
	if cancelValue, ok := h.generations.Load(binding.SessionID + ":" + messageID); ok {
		cancelValue.(context.CancelFunc)()
	}
	_ = h.streams.AppendEvent(c.Request.Context(), binding.SessionID, messageID, interfaces.StreamEvent{ID: uuid.NewString(), Type: types.ResponseType(event.EventStop), Done: true, Timestamp: time.Now(), Data: map[string]any{"reason": "integration_cancel"}})
	message.IsCompleted = true
	_ = h.messages.UpdateMessage(c.Request.Context(), message)
	_, _ = h.service.AppendStreamEvent(c.Request.Context(), binding.SessionID, messageID, "error", gin.H{"code": "cancelled", "retryable": false, "status": "cancelled"})
	h.audit(c, "api.chat.message.cancel", "allowed", "")
	integrationData(c, http.StatusOK, gin.H{"status": "cancelled"})
}

func (h *IntegrationHandler) CreateClient(c *gin.Context) {
	actor, _ := c.Get(types.UserContextKey.String())
	user, _ := actor.(*types.User)
	var req struct {
		ID                      string            `json:"id"`
		Name                    string            `json:"name" binding:"required"`
		TenantID                uint64            `json:"tenant_id" binding:"required"`
		IdentityProviderID      string            `json:"identity_provider_id" binding:"required"`
		AdministratorUserID     string            `json:"administrator_user_id"`
		Scopes                  []string          `json:"scopes"`
		KnowledgeBaseAccessMode string            `json:"knowledge_base_access_mode"`
		KnowledgeBaseIDs        []string          `json:"knowledge_base_ids"`
		AllowedOrigins          []string          `json:"allowed_origins" binding:"required"`
		RoleMappings            map[string]string `json:"role_mappings"`
		MaxRole                 string            `json:"max_role"`
	}
	if c.ShouldBindJSON(&req) != nil || len(req.AllowedOrigins) == 0 {
		integrationError(c, http.StatusBadRequest, "invalid_client", "invalid integration client")
		return
	}
	encode := func(value any) string { data, _ := jsonMarshal(value); return data }
	client := &integrationauth.Client{ID: req.ID, Name: req.Name, TenantID: req.TenantID, IdentityProviderID: req.IdentityProviderID, AdministratorUserID: req.AdministratorUserID, ScopesJSON: encode(req.Scopes), KnowledgeBaseAccessMode: req.KnowledgeBaseAccessMode, KnowledgeBaseIDsJSON: encode(req.KnowledgeBaseIDs), AllowedOriginsJSON: encode(req.AllowedOrigins), RoleMappingsJSON: encode(req.RoleMappings), MaxRole: req.MaxRole}
	secret, err := h.service.CreateClient(c.Request.Context(), user, client, "")
	if err != nil {
		status := http.StatusForbidden
		if errors.Is(err, integrationauth.ErrInvalid) {
			status = http.StatusBadRequest
		}
		integrationError(c, status, "client_create_failed", "integration client creation denied")
		return
	}
	integrationData(c, http.StatusCreated, gin.H{"client_id": client.ID, "client_secret": secret})
}

func (h *IntegrationHandler) ListClients(c *gin.Context) {
	actor, _ := c.Get(types.UserContextKey.String())
	user, _ := actor.(*types.User)
	clients, err := h.service.ListClients(c.Request.Context(), user)
	if err != nil {
		integrationError(c, http.StatusForbidden, "client_list_failed", "client list denied")
		return
	}
	result := make([]gin.H, 0, len(clients))
	for _, client := range clients {
		result = append(result, gin.H{"id": client.ID, "name": client.Name, "tenant_id": client.TenantID, "identity_provider_id": client.IdentityProviderID, "administrator_user_id": client.AdministratorUserID, "scopes": client.Scopes(), "knowledge_base_access_mode": client.KnowledgeBaseAccessMode, "knowledge_base_ids": client.KnowledgeBaseIDs(), "allowed_origins": client.AllowedOrigins(), "enabled": client.Enabled, "expires_at": client.ExpiresAt})
	}
	integrationData(c, http.StatusOK, result)
}

func (h *IntegrationHandler) UpdateClientScopes(c *gin.Context) {
	actor, _ := c.Get(types.UserContextKey.String())
	user, _ := actor.(*types.User)
	var req struct {
		Scopes []string `json:"scopes" binding:"required"`
	}
	if c.ShouldBindJSON(&req) != nil {
		integrationError(c, http.StatusBadRequest, "invalid_client_scopes", "scopes are required")
		return
	}
	if err := h.service.UpdateClientScopes(c.Request.Context(), user, c.Param("client_id"), req.Scopes); err != nil {
		integrationError(c, http.StatusForbidden, "client_scopes_update_failed", "integration client scope update denied")
		return
	}
	integrationData(c, http.StatusOK, gin.H{"scopes": req.Scopes, "reauthentication_required": true})
}

func (h *IntegrationHandler) UpdateClientKnowledgeBases(c *gin.Context) {
	actor, _ := c.Get(types.UserContextKey.String())
	user, _ := actor.(*types.User)
	var req struct {
		KnowledgeBaseIDs []string `json:"knowledge_base_ids" binding:"required"`
	}
	if c.ShouldBindJSON(&req) != nil {
		integrationError(c, http.StatusBadRequest, "invalid_knowledge_base_ids", "knowledge_base_ids are required")
		return
	}
	if err := h.service.UpdateClientKnowledgeBases(c.Request.Context(), user, c.Param("client_id"), req.KnowledgeBaseIDs); err != nil {
		status := http.StatusForbidden
		if errors.Is(err, integrationauth.ErrInvalid) {
			status = http.StatusBadRequest
		}
		integrationError(c, status, "client_knowledge_bases_update_failed", "integration client knowledge base update denied")
		return
	}
	integrationData(c, http.StatusOK, gin.H{"knowledge_base_ids": req.KnowledgeBaseIDs, "reauthentication_required": true})
}

func (h *IntegrationHandler) CreateIdentityProvider(c *gin.Context) {
	actor, _ := c.Get(types.UserContextKey.String())
	user, _ := actor.(*types.User)
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name" binding:"required"`
	}
	if c.ShouldBindJSON(&req) != nil {
		integrationError(c, http.StatusBadRequest, "invalid_identity_provider", "name is required")
		return
	}
	provider := &integrationauth.IdentityProvider{ID: req.ID, Name: req.Name}
	if err := h.service.CreateIdentityProvider(c.Request.Context(), user, provider); err != nil {
		integrationError(c, http.StatusForbidden, "identity_provider_create_failed", "identity provider create denied")
		return
	}
	integrationData(c, http.StatusCreated, provider)
}

func (h *IntegrationHandler) ListIdentityProviders(c *gin.Context) {
	actor, _ := c.Get(types.UserContextKey.String())
	user, _ := actor.(*types.User)
	providers, err := h.service.ListIdentityProviders(c.Request.Context(), user)
	if err != nil {
		integrationError(c, http.StatusForbidden, "identity_provider_list_failed", "identity provider list denied")
		return
	}
	integrationData(c, http.StatusOK, providers)
}

func jsonMarshal(value any) (string, error) {
	data, err := json.Marshal(value)
	return string(data), err
}

func (h *IntegrationHandler) RotateClientSecret(c *gin.Context) {
	actor, _ := c.Get(types.UserContextKey.String())
	user, _ := actor.(*types.User)
	secret, err := h.service.RotateSecret(c.Request.Context(), user, c.Param("client_id"))
	if err != nil {
		integrationError(c, http.StatusForbidden, "rotate_failed", "secret rotation failed")
		return
	}
	integrationData(c, http.StatusOK, gin.H{"client_secret": secret})
}

func (h *IntegrationHandler) RevealClientSecret(c *gin.Context) {
	actor, _ := c.Get(types.UserContextKey.String())
	user, _ := actor.(*types.User)
	secret, err := h.service.RevealClientSecret(c.Request.Context(), user, c.Param("client_id"))
	if err != nil {
		status := http.StatusForbidden
		if errors.Is(err, integrationauth.ErrInvalid) {
			status = http.StatusNotFound
		}
		integrationError(c, status, "secret_reveal_failed", "client secret is unavailable; rotate to generate a recoverable secret")
		return
	}
	integrationData(c, http.StatusOK, gin.H{"client_secret": secret})
}

func (h *IntegrationHandler) RevokePreviousClientSecret(c *gin.Context) {
	actor, _ := c.Get(types.UserContextKey.String())
	user, _ := actor.(*types.User)
	if err := h.service.RevokePreviousSecret(c.Request.Context(), user, c.Param("client_id")); err != nil {
		integrationError(c, http.StatusForbidden, "revoke_secret_failed", "previous secret revocation failed")
		return
	}
	integrationData(c, http.StatusOK, gin.H{"revoked": true})
}

func (h *IntegrationHandler) DisableClient(c *gin.Context) {
	actor, _ := c.Get(types.UserContextKey.String())
	user, _ := actor.(*types.User)
	if err := h.service.SetClientEnabled(c.Request.Context(), user, c.Param("client_id"), false); err != nil {
		integrationError(c, http.StatusForbidden, "disable_failed", "client disable failed")
		return
	}
	integrationData(c, http.StatusOK, gin.H{"enabled": false})
}

func (h *IntegrationHandler) BindClientAdministrator(c *gin.Context) {
	actor, _ := c.Get(types.UserContextKey.String())
	user, _ := actor.(*types.User)
	var req struct {
		UserID string `json:"user_id" binding:"required"`
	}
	if c.ShouldBindJSON(&req) != nil {
		integrationError(c, http.StatusBadRequest, "invalid_administrator", "user_id is required")
		return
	}
	if err := h.service.BindClientAdministrator(c.Request.Context(), user, c.Param("client_id"), req.UserID); err != nil {
		integrationError(c, http.StatusForbidden, "administrator_bind_failed", "administrator binding denied")
		return
	}
	integrationData(c, http.StatusOK, gin.H{"administrator_user_id": req.UserID})
}
