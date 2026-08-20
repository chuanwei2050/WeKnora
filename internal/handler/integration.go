package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/handler/session"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	integrationauth "github.com/Tencent/WeKnora/internal/integration"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type IntegrationHandler struct {
	service     *integrationauth.Service
	kbs         interfaces.KnowledgeBaseService
	sessions    interfaces.SessionService
	messages    interfaces.MessageService
	streams     interfaces.StreamManager
	attachments *session.AttachmentProcessor
	limiter     *integrationRateLimiter
	limits      integrationLimits
	generations sync.Map
}

func NewIntegrationHandler(service *integrationauth.Service, kbs interfaces.KnowledgeBaseService, sessions interfaces.SessionService, messages interfaces.MessageService, streams interfaces.StreamManager, files interfaces.FileService, models interfaces.ModelService, documents interfaces.DocumentReader, imageResolver *docparser.ImageResolver) *IntegrationHandler {
	return &IntegrationHandler{
		service: service, kbs: kbs, sessions: sessions, messages: messages, streams: streams,
		attachments: session.NewAttachmentProcessor(files, documents, imageResolver, models),
		limiter:     newIntegrationRateLimiter(), limits: loadIntegrationLimits(),
	}
}

type integrationLimits struct {
	maxKnowledgeBases int
	defaultTopK       int
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
		defaultTopK:       positiveEnv("INTEGRATION_DEFAULT_TOP_K", 10),
		maxTopK:           positiveEnv("INTEGRATION_MAX_TOP_K", 50),
		maxQueryBytes:     positiveEnv("INTEGRATION_MAX_QUERY_BYTES", 8192),
		maxRequestBytes:   positiveEnv("INTEGRATION_MAX_REQUEST_BYTES", 25*1024*1024),
	}
	if limits.defaultTopK > limits.maxTopK {
		limits.defaultTopK = limits.maxTopK
	}
	return limits
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
		Origin           string   `json:"origin" binding:"required"`
	}
	if c.ShouldBindJSON(&req) != nil {
		integrationError(c, http.StatusBadRequest, "invalid_request", "external identity and origin are required")
		return
	}
	ticket, err := h.service.CreateBootstrap(c.Request.Context(), bearerToken(c), integrationauth.BootstrapRequest{ExternalTenantID: req.ExternalTenantID, ExternalUserID: req.ExternalUserID, ExternalRoles: req.ExternalRoles, Origin: req.Origin})
	if err != nil {
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
	c.SetCookie(integrationauth.BrowserCookieName, token, int(integrationauth.SessionMaxTTL.Seconds()), "/", "", true, true)
	integrationData(c, http.StatusOK, gin.H{"csrf_token": csrf, "user": user, "knowledge_base_ids": principal.KnowledgeBaseIDs})
}

func (h *IntegrationHandler) Refresh(c *gin.Context) {
	if !h.enforceRate(c, "refresh", 120) {
		return
	}
	token, err := c.Cookie(integrationauth.BrowserCookieName)
	if err != nil {
		integrationError(c, http.StatusUnauthorized, "missing_session", "session cookie is required")
		return
	}
	if err = h.service.ValidateBrowserOrigin(c.Request.Context(), token, c.GetHeader("Origin")); err != nil {
		h.service.Audit(c.Request.Context(), nil, "auth.refresh", "denied", "origin_denied")
		integrationError(c, http.StatusForbidden, "origin_denied", "browser origin denied")
		return
	}
	if err = h.service.ValidateCSRF(c.Request.Context(), token, c.GetHeader("X-CSRF-Token")); err != nil {
		h.service.Audit(c.Request.Context(), nil, "auth.refresh", "denied", "csrf_failed")
		integrationError(c, http.StatusForbidden, "csrf_failed", "CSRF validation failed")
		return
	}
	csrf, err := h.service.Refresh(c.Request.Context(), token, c.GetHeader("X-CSRF-Token"))
	if err != nil {
		h.service.Audit(c.Request.Context(), nil, "auth.refresh", "denied", "session_expired")
		integrationError(c, http.StatusUnauthorized, "session_expired", "session refresh failed")
		return
	}
	integrationData(c, http.StatusOK, gin.H{"csrf_token": csrf})
}

func (h *IntegrationHandler) Logout(c *gin.Context) {
	token, _ := c.Cookie(integrationauth.BrowserCookieName)
	if token != "" {
		if h.service.ValidateBrowserOrigin(c.Request.Context(), token, c.GetHeader("Origin")) != nil || h.service.ValidateCSRF(c.Request.Context(), token, c.GetHeader("X-CSRF-Token")) != nil {
			integrationError(c, http.StatusForbidden, "logout_denied", "origin or CSRF validation failed")
			return
		}
		_ = h.service.Logout(c.Request.Context(), token)
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(integrationauth.BrowserCookieName, "", -1, "/", "", true, true)
	integrationData(c, http.StatusOK, gin.H{"logged_out": true})
}

func (h *IntegrationHandler) Authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, cookieErr := c.Cookie(integrationauth.BrowserCookieName)
		kind := "browser"
		if cookieErr != nil {
			token = bearerToken(c)
			kind = "service"
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

func (h *IntegrationHandler) Search(c *gin.Context) {
	if !h.enforceRate(c, "api.search", 60) {
		return
	}
	if !h.requireScope(c, "rag:search") {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(h.limits.maxRequestBytes))
	var req struct {
		KnowledgeBaseIDs *[]string `json:"knowledge_base_ids" binding:"required"`
		Query            string    `json:"query" binding:"required"`
		TopK             int       `json:"top_k"`
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
	if h.service.AuthorizeKnowledgeBases(integrationPrincipal(c), *req.KnowledgeBaseIDs) != nil {
		h.service.AuditResources(c.Request.Context(), integrationPrincipal(c), "api.search", "denied", "knowledge_base_denied", *req.KnowledgeBaseIDs)
		integrationError(c, http.StatusForbidden, "knowledge_base_denied", "knowledge base access denied")
		return
	}
	results, err := h.sessions.SearchKnowledge(c.Request.Context(), *req.KnowledgeBaseIDs, nil, req.Query)
	if err != nil {
		integrationError(c, http.StatusInternalServerError, "search_failed", "knowledge search failed")
		return
	}
	limit := req.TopK
	if limit == 0 {
		limit = h.limits.defaultTopK
	}
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
		h.audit(c, "api.chat.session.create", "allowed", "idempotent_replay")
		integrationData(c, http.StatusOK, existing)
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
	integrationData(c, http.StatusCreated, gin.H{"id": session.ID, "title": session.Title, "knowledge_base_mode": req.KnowledgeBaseMode, "allowed_knowledge_base_ids": allowed})
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
		SelectedKnowledgeBaseIDs *[]string `json:"selected_knowledge_base_ids"`
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
	_, _ = h.service.AppendStreamEvent(c.Request.Context(), binding.SessionID, assistantMessage.ID, "message.created", gin.H{"user_message_id": userMessage.ID, "selected_knowledge_base_ids": selected})
	eventBus := event.NewEventBus()
	var answer strings.Builder
	var references any = []any{}
	generationDone := make(chan error, 1)
	var generationDoneOnce sync.Once
	eventBus.On(event.EventAgentFinalAnswer, func(ctx context.Context, evt event.Event) error {
		data, ok := evt.Data.(event.AgentFinalAnswerData)
		if !ok {
			return nil
		}
		if data.Content != "" {
			answer.WriteString(data.Content)
			_, _ = h.service.AppendStreamEvent(ctx, binding.SessionID, assistantMessage.ID, "answer.delta", gin.H{"content": data.Content})
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
			references = data.References
		}
		return nil
	})
	generationCtx, cancelGeneration := context.WithCancel(c.Request.Context())
	generationKey := binding.SessionID + ":" + assistantMessage.ID
	h.generations.Store(generationKey, cancelGeneration)
	defer func() {
		cancelGeneration()
		h.generations.Delete(generationKey)
	}()
	session, err := h.sessions.GetSession(generationCtx, binding.SessionID)
	if err == nil {
		err = h.sessions.KnowledgeQA(generationCtx, &types.QARequest{Session: session, Query: req.Query, AssistantMessageID: assistantMessage.ID, KnowledgeBaseIDs: selected, UserMessageID: userMessage.ID, ImageURLs: imageURLs, Attachments: processedAttachments}, eventBus)
	}
	if err == nil {
		select {
		case err = <-generationDone:
		case <-generationCtx.Done():
			err = generationCtx.Err()
		}
	}
	if generationCtx.Err() != nil {
		h.writeStoredEvents(c, binding, assistantMessage.ID, "")
		return
	}
	if err != nil {
		_, _ = h.service.AppendStreamEvent(c.Request.Context(), binding.SessionID, assistantMessage.ID, "error", gin.H{"code": "generation_failed", "retryable": false, "status": "failed"})
		integrationError(c, http.StatusInternalServerError, "generation_failed", "answer generation failed")
		return
	}
	assistantMessage.Content = answer.String()
	assistantMessage.IsCompleted = true
	if err = h.messages.UpdateMessage(c.Request.Context(), assistantMessage); err != nil {
		_, _ = h.service.AppendStreamEvent(c.Request.Context(), binding.SessionID, assistantMessage.ID, "error", gin.H{"code": "message_persist_failed", "retryable": true, "status": "failed"})
		h.writeStoredEvents(c, binding, assistantMessage.ID, "")
		return
	}
	_, _ = h.service.AppendStreamEvent(c.Request.Context(), binding.SessionID, assistantMessage.ID, "answer.completed", gin.H{"status": "completed", "answer": assistantMessage.Content, "selected_knowledge_base_ids": selected, "references": references})
	h.service.AuditResources(c.Request.Context(), principal, "api.chat.message.create", "allowed", "", selected)
	h.writeStoredEvents(c, binding, assistantMessage.ID, "")
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
		envelope := gin.H{"event_id": stored.EventID, "event": stored.Event, "session_id": stored.SessionID, "message_id": stored.MessageID, "sequence": stored.Sequence, "occurred_at": stored.OccurredAt, "data": json.RawMessage(stored.DataJSON)}
		data, _ := json.Marshal(envelope)
		_, _ = c.Writer.Write([]byte("id: " + stored.EventID + "\nevent: " + stored.Event + "\ndata: " + string(data) + "\n\n"))
	}
	c.Writer.Flush()
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
		ID                 string            `json:"id"`
		Name               string            `json:"name" binding:"required"`
		TenantID           uint64            `json:"tenant_id" binding:"required"`
		IdentityProviderID string            `json:"identity_provider_id" binding:"required"`
		Scopes             []string          `json:"scopes"`
		KnowledgeBaseIDs   []string          `json:"knowledge_base_ids"`
		AllowedOrigins     []string          `json:"allowed_origins" binding:"required"`
		RoleMappings       map[string]string `json:"role_mappings"`
		MaxRole            string            `json:"max_role"`
	}
	if c.ShouldBindJSON(&req) != nil || len(req.AllowedOrigins) == 0 {
		integrationError(c, http.StatusBadRequest, "invalid_client", "invalid integration client")
		return
	}
	encode := func(value any) string { data, _ := jsonMarshal(value); return data }
	client := &integrationauth.Client{ID: req.ID, Name: req.Name, TenantID: req.TenantID, IdentityProviderID: req.IdentityProviderID, ScopesJSON: encode(req.Scopes), KnowledgeBaseIDsJSON: encode(req.KnowledgeBaseIDs), AllowedOriginsJSON: encode(req.AllowedOrigins), RoleMappingsJSON: encode(req.RoleMappings), MaxRole: req.MaxRole}
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
		result = append(result, gin.H{"id": client.ID, "name": client.Name, "tenant_id": client.TenantID, "identity_provider_id": client.IdentityProviderID, "enabled": client.Enabled, "expires_at": client.ExpiresAt})
	}
	integrationData(c, http.StatusOK, result)
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
