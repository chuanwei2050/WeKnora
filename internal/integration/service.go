package integration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var (
	ErrUnauthorized = errors.New("integration authentication failed")
	ErrForbidden    = errors.New("integration access denied")
	ErrConflict     = errors.New("integration credential already consumed")
	ErrInvalid      = errors.New("invalid integration request")
)

const (
	BrowserCookieName = "weknora_integration_session"
	TicketTTL         = time.Minute
	SessionTTL        = 15 * time.Minute
	SessionMaxTTL     = 8 * time.Hour
	ServiceTokenTTL   = 10 * time.Minute
)

type Service struct {
	db             *gorm.DB
	users          interfaces.UserService
	tenants        interfaces.TenantService
	now            func() time.Time
	eventRetention time.Duration
	ticketMu       sync.Mutex
	eventMu        sync.Mutex
}

func NewService(db *gorm.DB, users interfaces.UserService, tenants interfaces.TenantService) *Service {
	retention := time.Hour
	if configured, err := time.ParseDuration(strings.TrimSpace(os.Getenv("INTEGRATION_EVENT_RETENTION"))); err == nil && configured > 0 {
		retention = configured
	}
	return &Service{db: db, users: users, tenants: tenants, now: time.Now, eventRetention: retention}
}

func (s *Service) CreateIdentityProvider(ctx context.Context, actor *types.User, provider *IdentityProvider) error {
	if actor == nil || !actor.IsPlatformAdmin() {
		return ErrForbidden
	}
	if provider.ID == "" {
		provider.ID = uuid.NewString()
	}
	if strings.TrimSpace(provider.Name) == "" {
		return ErrForbidden
	}
	return s.db.WithContext(ctx).Create(provider).Error
}

func (s *Service) ListIdentityProviders(ctx context.Context, actor *types.User) ([]IdentityProvider, error) {
	if actor == nil || !actor.IsPlatformAdmin() {
		return nil, ErrForbidden
	}
	var providers []IdentityProvider
	return providers, s.db.WithContext(ctx).Order("created_at DESC").Find(&providers).Error
}

func (s *Service) ListClients(ctx context.Context, actor *types.User) ([]Client, error) {
	if actor == nil || !actor.IsPlatformAdmin() {
		return nil, ErrForbidden
	}
	var clients []Client
	return clients, s.db.WithContext(ctx).Order("created_at DESC").Find(&clients).Error
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func digestMatches(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func encodeStrings(values []string) string { data, _ := json.Marshal(values); return string(data) }
func decodeStrings(value string) []string {
	var result []string
	_ = json.Unmarshal([]byte(value), &result)
	return result
}

func parseStringArray(value string) ([]string, error) {
	var result []string
	if err := json.Unmarshal([]byte(value), &result); err != nil || result == nil {
		return nil, ErrInvalid
	}
	for _, item := range result {
		if strings.TrimSpace(item) == "" {
			return nil, ErrInvalid
		}
	}
	return result, nil
}
func validClient(client *Client, now time.Time) bool {
	return client != nil && client.Enabled && (client.ExpiresAt == nil || client.ExpiresAt.After(now))
}

var allowedClientScopes = []string{"kb:list", "rag:search", "table:analyze", "chat:read", "chat:write", "knowledge:read", "knowledge:write", "file:read"}

func validateClientScopes(scopes []string) error {
	for _, scope := range scopes {
		if !slices.Contains(allowedClientScopes, scope) {
			return ErrForbidden
		}
	}
	return nil
}

func (s *Service) CreateClient(ctx context.Context, actor *types.User, client *Client, secret string) (string, error) {
	if actor == nil || !actor.IsPlatformAdmin() {
		return "", ErrForbidden
	}
	if client.ID == "" {
		client.ID = uuid.NewString()
	}
	if client.MaxRole == "" {
		client.MaxRole = string(types.UserRoleMember)
	}
	if !types.IsTenantUserRole(types.UserRole(client.MaxRole)) {
		return "", ErrForbidden
	}
	if client.RoleMappingsJSON == "" {
		client.RoleMappingsJSON = `{}`
	}
	if client.ScopesJSON == "" {
		client.ScopesJSON = "[]"
	}
	if client.KnowledgeBaseIDsJSON == "" {
		client.KnowledgeBaseIDsJSON = "[]"
	}
	scopes, err := parseStringArray(client.ScopesJSON)
	if err != nil {
		return "", err
	}
	if err = validateClientScopes(scopes); err != nil {
		return "", err
	}
	knowledgeBaseIDs, err := parseStringArray(client.KnowledgeBaseIDsJSON)
	if err != nil {
		return "", err
	}
	if len(knowledgeBaseIDs) > 0 {
		var count int64
		if err = s.db.WithContext(ctx).Table("knowledge_bases").Where("id IN ? AND tenant_id = ? AND deleted_at IS NULL", knowledgeBaseIDs, client.TenantID).Count(&count).Error; err != nil || count != int64(len(knowledgeBaseIDs)) {
			return "", ErrForbidden
		}
	}
	origins, err := parseStringArray(client.AllowedOriginsJSON)
	if err != nil || len(origins) == 0 {
		return "", ErrInvalid
	}
	var mappings map[string]string
	if json.Unmarshal([]byte(client.RoleMappingsJSON), &mappings) != nil {
		return "", ErrForbidden
	}
	hasAdministratorMapping := false
	for _, mapped := range mappings {
		if !types.IsTenantUserRole(types.UserRole(mapped)) {
			return "", ErrForbidden
		}
		hasAdministratorMapping = hasAdministratorMapping || types.UserRole(mapped) == types.UserRoleTenantAdmin
	}
	if hasAdministratorMapping {
		if client.AdministratorUserID == "" {
			return "", ErrInvalid
		}
		var administrator types.User
		if err = s.db.WithContext(ctx).First(&administrator, "id = ?", client.AdministratorUserID).Error; err != nil || administrator.TenantID != client.TenantID || !administrator.IsActive || administrator.EffectiveRole() != types.UserRoleTenantAdmin {
			return "", ErrForbidden
		}
	}
	var provider IdentityProvider
	if s.db.WithContext(ctx).First(&provider, "id = ?", client.IdentityProviderID).Error != nil {
		return "", ErrForbidden
	}
	for _, origin := range origins {
		parsed, err := url.ParseRequestURI(origin)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", fmt.Errorf("invalid allowed origin: %s", origin)
		}
	}
	if secret == "" {
		var err error
		secret, err = randomToken()
		if err != nil {
			return "", err
		}
	} else if len(secret) < 12 {
		return "", ErrInvalid
	}
	client.SecretHash = digest(secret)
	client.Enabled = true
	return secret, s.db.WithContext(ctx).Create(client).Error
}

func (s *Service) UpdateClientScopes(ctx context.Context, actor *types.User, clientID string, scopes []string) error {
	if actor == nil || !actor.IsPlatformAdmin() || strings.TrimSpace(clientID) == "" {
		return ErrForbidden
	}
	if err := validateClientScopes(scopes); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&Client{}).Where("id = ?", clientID).Update("scopes_json", encodeStrings(scopes))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrForbidden
		}
		return tx.Model(&Session{}).Where("client_id = ? AND revoked_at IS NULL", clientID).Update("revoked_at", s.now()).Error
	})
}

func (s *Service) RotateSecret(ctx context.Context, actor *types.User, clientID string) (string, error) {
	if actor == nil || !actor.IsPlatformAdmin() {
		return "", ErrForbidden
	}
	secret, err := randomToken()
	if err != nil {
		return "", err
	}
	result := s.db.WithContext(ctx).Model(&Client{}).Where("id = ?", clientID).
		Updates(map[string]any{"previous_secret_hash": gorm.Expr("secret_hash"), "secret_hash": digest(secret)})
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected != 1 {
		return "", ErrForbidden
	}
	return secret, nil
}

func (s *Service) RevokePreviousSecret(ctx context.Context, actor *types.User, clientID string) error {
	if actor == nil || !actor.IsPlatformAdmin() {
		return ErrForbidden
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&Client{}).Where("id = ?", clientID).Update("previous_secret_hash", "")
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrForbidden
		}
		return tx.Model(&Session{}).Where("client_id = ? AND revoked_at IS NULL", clientID).Update("revoked_at", s.now()).Error
	})
}

func (s *Service) SetClientEnabled(ctx context.Context, actor *types.User, clientID string, enabled bool) error {
	if actor == nil || !actor.IsPlatformAdmin() {
		return ErrForbidden
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&Client{}).Where("id = ?", clientID).Update("enabled", enabled)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrForbidden
		}
		if !enabled {
			return tx.Model(&Session{}).Where("client_id = ? AND revoked_at IS NULL", clientID).Update("revoked_at", s.now()).Error
		}
		return nil
	})
}

func (s *Service) BindClientAdministrator(ctx context.Context, actor *types.User, clientID, userID string) error {
	if actor == nil || !actor.IsPlatformAdmin() || strings.TrimSpace(userID) == "" {
		return ErrForbidden
	}
	var client Client
	if err := s.db.WithContext(ctx).First(&client, "id = ?", clientID).Error; err != nil {
		return ErrForbidden
	}
	var administrator types.User
	if err := s.db.WithContext(ctx).First(&administrator, "id = ?", userID).Error; err != nil || administrator.TenantID != client.TenantID || !administrator.IsActive || administrator.EffectiveRole() != types.UserRoleTenantAdmin {
		return ErrForbidden
	}
	return s.db.WithContext(ctx).Model(&Client{}).Where("id = ?", clientID).Update("administrator_user_id", userID).Error
}

func (s *Service) IssueServiceToken(ctx context.Context, clientID, secret string) (string, time.Time, error) {
	var client Client
	if err := s.db.WithContext(ctx).First(&client, "id = ?", clientID).Error; err != nil || !validClient(&client, s.now()) {
		s.Audit(ctx, &Principal{ClientID: clientID}, "auth.token", "denied", "invalid_or_expired_client")
		return "", time.Time{}, ErrUnauthorized
	}
	auditPrincipal := &Principal{ClientID: client.ID, TenantID: client.TenantID}
	hash := digest(secret)
	if !digestMatches(hash, client.SecretHash) && !digestMatches(hash, client.PreviousSecretHash) {
		s.Audit(ctx, auditPrincipal, "auth.token", "denied", "invalid_client_secret")
		return "", time.Time{}, ErrUnauthorized
	}
	token, err := randomToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expires := s.now().Add(ServiceTokenTTL)
	session := Session{ID: uuid.NewString(), Digest: digest(token), Kind: "service", ClientID: client.ID, TenantID: client.TenantID, ScopesJSON: client.ScopesJSON, KnowledgeBaseIDsJSON: client.KnowledgeBaseIDsJSON, ExpiresAt: expires, AbsoluteExpiresAt: expires}
	err = s.db.WithContext(ctx).Create(&session).Error
	if err == nil {
		s.Audit(ctx, auditPrincipal, "auth.token", "allowed", "")
	}
	return token, expires, err
}

type BootstrapRequest struct {
	ExternalTenantID string
	ExternalUserID   string
	ExternalRoles    []string
	Active           *bool
	Origin           string
}

func (s *Service) CreateBootstrap(ctx context.Context, serviceToken string, req BootstrapRequest) (string, error) {
	principal, _, _, err := s.Authenticate(ctx, serviceToken, "service")
	if err != nil {
		return "", err
	}
	var client Client
	if err = s.db.WithContext(ctx).First(&client, "id = ?", principal.ClientID).Error; err != nil {
		return "", ErrUnauthorized
	}
	if !slices.Contains(decodeStrings(client.AllowedOriginsJSON), req.Origin) {
		s.Audit(ctx, principal, "auth.bootstrap", "denied", "origin_denied")
		return "", ErrForbidden
	}
	user, err := s.resolveExternalUser(ctx, &client, req)
	if err != nil {
		return "", err
	}
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	allowed := make([]string, 0)
	for _, knowledgeBaseID := range decodeStrings(client.KnowledgeBaseIDsJSON) {
		if user.CanAccessKnowledgeBase(knowledgeBaseID) {
			allowed = append(allowed, knowledgeBaseID)
		}
	}
	ticket := BootstrapTicket{Digest: digest(token), JTI: uuid.NewString(), ClientID: client.ID, UserID: user.ID, Origin: req.Origin, KnowledgeBaseIDsJSON: encodeStrings(allowed), ExpiresAt: s.now().Add(TicketTTL)}
	err = s.db.WithContext(ctx).Create(&ticket).Error
	if err == nil {
		s.Audit(ctx, principal, "auth.bootstrap", "allowed", "")
	}
	return token, err
}

func (s *Service) resolveExternalUser(ctx context.Context, client *Client, req BootstrapRequest) (*types.User, error) {
	requestedActive := req.Active == nil || *req.Active
	var identity ExternalIdentity
	err := s.db.WithContext(ctx).Where("client_id = ? AND external_tenant_id = ? AND external_user_id = ?", client.ID, req.ExternalTenantID, req.ExternalUserID).First(&identity).Error
	var declaredMappings map[string]string
	_ = json.Unmarshal([]byte(client.RoleMappingsJSON), &declaredMappings)
	for _, externalRole := range req.ExternalRoles {
		if _, declared := declaredMappings[externalRole]; !declared {
			return nil, ErrForbidden
		}
	}
	role := mappedRole(client, req.ExternalRoles)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = s.db.WithContext(ctx).Where("client_id = '' AND identity_provider_id = ? AND external_tenant_id = ? AND external_user_id = ?", client.IdentityProviderID, req.ExternalTenantID, req.ExternalUserID).First(&identity).Error
		if err == nil {
			if role == types.UserRoleTenantAdmin && identity.UserID != client.AdministratorUserID {
				return nil, ErrForbidden
			}
			if updateErr := s.db.WithContext(ctx).Model(&identity).Updates(map[string]any{"client_id": client.ID, "active": requestedActive}).Error; updateErr != nil {
				return nil, updateErr
			}
			identity.ClientID = client.ID
			identity.Active = requestedActive
		}
	}
	if err == nil {
		if !requestedActive {
			if updateErr := s.disableExternalIdentity(ctx, client.ID, identity.UserID); updateErr != nil {
				return nil, updateErr
			}
			return nil, ErrForbidden
		}
		if !identity.Active {
			if updateErr := s.db.WithContext(ctx).Model(&identity).Update("active", true).Error; updateErr != nil {
				return nil, updateErr
			}
		}
		if role == types.UserRoleTenantAdmin && identity.UserID != client.AdministratorUserID {
			return nil, ErrForbidden
		}
		user, getErr := s.users.GetUserByID(ctx, identity.UserID)
		if getErr != nil || user == nil {
			return user, getErr
		}
		if user.TenantID != client.TenantID || !user.IsActive || user.EffectiveRole() != role {
			return nil, ErrForbidden
		}
		return user, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if !requestedActive {
		return nil, ErrForbidden
	}
	if role == types.UserRoleTenantAdmin {
		if client.AdministratorUserID == "" {
			return nil, ErrForbidden
		}
		user, getErr := s.users.GetUserByID(ctx, client.AdministratorUserID)
		if getErr != nil || user == nil || user.TenantID != client.TenantID || !user.IsActive || user.EffectiveRole() != types.UserRoleTenantAdmin {
			return nil, ErrForbidden
		}
		identity = ExternalIdentity{ClientID: client.ID, IdentityProviderID: client.IdentityProviderID, ExternalTenantID: req.ExternalTenantID, ExternalUserID: req.ExternalUserID, UserID: user.ID, Active: true}
		return user, s.db.WithContext(ctx).Create(&identity).Error
	}
	userID := uuid.NewString()
	nameDigest := digest(client.ID + ":" + req.ExternalTenantID + ":" + req.ExternalUserID)[:20]
	randomPassword, err := randomToken()
	if err != nil {
		return nil, err
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(randomPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	user := &types.User{ID: userID, Username: "integration-" + nameDigest, Email: "integration-" + nameDigest + "@external.local", PasswordHash: string(passwordHash), TenantID: client.TenantID, IsActive: true, Role: role, KnowledgeBaseAccessMode: types.KnowledgeBaseAccessSelected, KnowledgeBaseIDs: decodeStrings(client.KnowledgeBaseIDsJSON)}
	return user, s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(user).Error; err != nil {
			return err
		}
		return tx.Create(&ExternalIdentity{ClientID: client.ID, IdentityProviderID: client.IdentityProviderID, ExternalTenantID: req.ExternalTenantID, ExternalUserID: req.ExternalUserID, UserID: user.ID, Active: true}).Error
	})
}

func (s *Service) disableExternalIdentity(ctx context.Context, clientID, userID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&ExternalIdentity{}).Where("client_id = ? AND user_id = ?", clientID, userID).Update("active", false).Error; err != nil {
			return err
		}
		return tx.Model(&Session{}).Where("client_id = ? AND user_id = ? AND revoked_at IS NULL", clientID, userID).Update("revoked_at", s.now()).Error
	})
}

func mappedRole(client *Client, externalRoles []string) types.UserRole {
	role := types.UserRoleMember
	if client == nil || client.MaxRole == string(types.UserRoleMember) {
		return role
	}
	var mappings map[string]string
	_ = json.Unmarshal([]byte(client.RoleMappingsJSON), &mappings)
	for _, externalRole := range externalRoles {
		if types.UserRole(mappings[externalRole]) == types.UserRoleTenantAdmin {
			return types.UserRoleTenantAdmin
		}
	}
	return role
}

func (s *Service) Exchange(ctx context.Context, ticketToken, origin string) (string, string, *Principal, *types.User, error) {
	ticket, err := s.consumeTicket(ctx, ticketToken, origin)
	if err != nil {
		return "", "", nil, nil, err
	}
	var client Client
	if err = s.db.WithContext(ctx).First(&client, "id = ?", ticket.ClientID).Error; err != nil || !validClient(&client, s.now()) {
		return "", "", nil, nil, ErrUnauthorized
	}
	user, err := s.users.GetUserByID(ctx, ticket.UserID)
	if err != nil || user == nil || !user.IsActive {
		return "", "", nil, nil, ErrUnauthorized
	}
	token, err := randomToken()
	if err != nil {
		return "", "", nil, nil, err
	}
	csrf, err := randomToken()
	if err != nil {
		return "", "", nil, nil, err
	}
	now := s.now()
	session := Session{ID: uuid.NewString(), Digest: digest(token), Kind: "browser", ClientID: client.ID, TenantID: client.TenantID, UserID: user.ID, ScopesJSON: client.ScopesJSON, KnowledgeBaseIDsJSON: ticket.KnowledgeBaseIDsJSON, CSRFHash: digest(csrf), ExpiresAt: now.Add(SessionTTL), AbsoluteExpiresAt: now.Add(SessionMaxTTL)}
	if err = s.db.WithContext(ctx).Create(&session).Error; err != nil {
		return "", "", nil, nil, err
	}
	s.Audit(ctx, principalFromSession(&session), "auth.exchange", "allowed", "")
	return token, csrf, principalFromSession(&session), user, nil
}

func (s *Service) consumeTicket(ctx context.Context, ticketToken, origin string) (*BootstrapTicket, error) {
	s.ticketMu.Lock()
	defer s.ticketMu.Unlock()
	now := s.now()
	var result *gorm.DB
	for attempt := 0; attempt < 5; attempt++ {
		result = s.db.WithContext(ctx).Model(&BootstrapTicket{}).
			Where("digest = ? AND origin = ? AND expires_at > ? AND consumed_at IS NULL", digest(ticketToken), origin, now).
			Update("consumed_at", now)
		if result.Error == nil || !strings.Contains(strings.ToLower(result.Error.Error()), "locked") {
			break
		}
		time.Sleep(time.Duration(attempt+1) * time.Millisecond)
	}
	if result.Error != nil {
		return nil, result.Error
	}
	var ticket BootstrapTicket
	if result.RowsAffected == 1 {
		if err := s.db.WithContext(ctx).Where("digest = ?", digest(ticketToken)).First(&ticket).Error; err != nil {
			return nil, err
		}
		return &ticket, nil
	}
	if err := s.db.WithContext(ctx).Where("digest = ?", digest(ticketToken)).First(&ticket).Error; err != nil {
		return nil, ErrUnauthorized
	}
	if ticket.ConsumedAt != nil {
		return nil, ErrConflict
	}
	return nil, ErrUnauthorized
}

func principalFromSession(session *Session) *Principal {
	return &Principal{ClientID: session.ClientID, TenantID: session.TenantID, UserID: session.UserID, Scopes: decodeStrings(session.ScopesJSON), KnowledgeBaseIDs: decodeStrings(session.KnowledgeBaseIDsJSON), Kind: session.Kind}
}

// PrincipalFromBidReview adapts the legacy Bearer SSO identity to the same
// principal shape for downstream observability without changing its old login flow.
func PrincipalFromBidReview(user *types.User) *Principal {
	if user == nil || strings.TrimSpace(user.BidReviewRole) == "" {
		return nil
	}
	return &Principal{
		ClientID:         "bidreview-legacy",
		TenantID:         user.TenantID,
		UserID:           user.ID,
		Scopes:           []string{"legacy:bearer"},
		KnowledgeBaseIDs: slices.Clone([]string(user.KnowledgeBaseIDs)),
		Kind:             "bidreview-legacy",
	}
}

func intersectStrings(left, right []string) []string {
	result := make([]string, 0, len(left))
	for _, value := range left {
		if slices.Contains(right, value) && !slices.Contains(result, value) {
			result = append(result, value)
		}
	}
	return result
}

func (s *Service) Authenticate(ctx context.Context, token, kind string) (*Principal, *types.User, *types.Tenant, error) {
	var session Session
	if token == "" || s.db.WithContext(ctx).Where("digest = ? AND kind = ?", digest(token), kind).First(&session).Error != nil {
		return nil, nil, nil, ErrUnauthorized
	}
	now := s.now()
	if session.RevokedAt != nil || !session.ExpiresAt.After(now) || !session.AbsoluteExpiresAt.After(now) {
		return nil, nil, nil, ErrUnauthorized
	}
	var client Client
	if s.db.WithContext(ctx).First(&client, "id = ?", session.ClientID).Error != nil || !validClient(&client, now) {
		return nil, nil, nil, ErrUnauthorized
	}
	var user *types.User
	var err error
	if session.UserID != "" {
		var activeIdentities int64
		if s.db.WithContext(ctx).Model(&ExternalIdentity{}).Where("user_id = ? AND active = ? AND (client_id = ? OR client_id = '')", session.UserID, true, session.ClientID).Count(&activeIdentities).Error != nil || activeIdentities == 0 {
			return nil, nil, nil, ErrUnauthorized
		}
		user, err = s.users.GetUserByID(ctx, session.UserID)
		if err != nil || user == nil || !user.IsActive {
			return nil, nil, nil, ErrUnauthorized
		}
	}
	tenant, err := s.tenants.GetTenantByID(ctx, session.TenantID)
	if err != nil || tenant == nil || tenant.Status != string(types.TenantStatusActive) {
		return nil, nil, nil, ErrUnauthorized
	}
	principal := principalFromSession(&session)
	principal.Scopes = intersectStrings(principal.Scopes, decodeStrings(client.ScopesJSON))
	principal.KnowledgeBaseIDs = intersectStrings(principal.KnowledgeBaseIDs, decodeStrings(client.KnowledgeBaseIDsJSON))
	if user != nil {
		filtered := principal.KnowledgeBaseIDs[:0]
		for _, knowledgeBaseID := range principal.KnowledgeBaseIDs {
			if user.CanAccessKnowledgeBase(knowledgeBaseID) {
				filtered = append(filtered, knowledgeBaseID)
			}
		}
		principal.KnowledgeBaseIDs = filtered
	}
	return principal, user, tenant, nil
}

func (s *Service) ValidateCSRF(ctx context.Context, browserToken, csrf string) error {
	if csrf == "" {
		return ErrForbidden
	}
	var current Session
	if s.db.WithContext(ctx).Where("digest = ? AND kind = 'browser'", digest(browserToken)).First(&current).Error != nil {
		return ErrForbidden
	}
	if digestMatches(current.CSRFHash, digest(csrf)) {
		return nil
	}
	var matching Session
	now := s.now()
	err := s.db.WithContext(ctx).
		Where("csrf_hash = ? AND kind = 'browser' AND client_id = ? AND tenant_id = ? AND user_id = ? AND revoked_at IS NULL AND expires_at > ? AND absolute_expires_at > ?",
			digest(csrf), current.ClientID, current.TenantID, current.UserID, now, now).
		First(&matching).Error
	if err != nil {
		return ErrForbidden
	}
	return nil
}

func (s *Service) Refresh(ctx context.Context, browserToken, csrf string) (string, error) {
	principal, _, _, err := s.Authenticate(ctx, browserToken, "browser")
	if err != nil {
		return "", err
	}
	now := s.now()
	expires := now.Add(SessionTTL)
	var session Session
	if err = s.db.WithContext(ctx).Where("digest = ?", digest(browserToken)).First(&session).Error; err != nil {
		return "", err
	}
	if expires.After(session.AbsoluteExpiresAt) {
		expires = session.AbsoluteExpiresAt
	}
	err = s.db.WithContext(ctx).Model(&Session{}).Where("digest = ? AND client_id = ?", digest(browserToken), principal.ClientID).Update("expires_at", expires).Error
	if err == nil {
		s.Audit(ctx, principal, "auth.refresh", "allowed", "")
	}
	return csrf, err
}

func (s *Service) Logout(ctx context.Context, browserToken string) error {
	return s.db.WithContext(ctx).Model(&Session{}).Where("digest = ?", digest(browserToken)).Update("revoked_at", s.now()).Error
}

func (s *Service) ValidateBrowserOrigin(ctx context.Context, browserToken, origin string) error {
	if origin == "" {
		return ErrForbidden
	}
	var session Session
	if s.db.WithContext(ctx).Where("digest = ? AND kind = 'browser'", digest(browserToken)).First(&session).Error != nil {
		return ErrUnauthorized
	}
	var client Client
	if s.db.WithContext(ctx).First(&client, "id = ?", session.ClientID).Error != nil || !slices.Contains(decodeStrings(client.AllowedOriginsJSON), origin) {
		return ErrForbidden
	}
	return nil
}

// IsAllowedOrigin is used by CORS preflight, where no browser session cookie is
// available yet. The request still goes through the authenticated, client-bound
// origin check before business handling.
func (s *Service) IsAllowedOrigin(ctx context.Context, origin string) bool {
	if origin == "" {
		return false
	}
	var clients []Client
	if s.db.WithContext(ctx).Where("enabled = ?", true).Find(&clients).Error != nil {
		return false
	}
	now := s.now()
	for i := range clients {
		if validClient(&clients[i], now) && slices.Contains(decodeStrings(clients[i].AllowedOriginsJSON), origin) {
			return true
		}
	}
	return false
}

func (s *Service) Audit(ctx context.Context, principal *Principal, action, outcome, reason string) {
	s.audit(ctx, principal, action, outcome, reason, nil)
}

func (s *Service) AuditResources(ctx context.Context, principal *Principal, action, outcome, reason string, resourceIDs []string) {
	s.audit(ctx, principal, action, outcome, reason, resourceIDs)
}

func (s *Service) audit(ctx context.Context, principal *Principal, action, outcome, reason string, resourceIDs []string) {
	record := &Audit{Action: action, Outcome: outcome, Reason: reason, ScopesJSON: "[]", KnowledgeBaseIDsJSON: "[]", ResourceIDsJSON: encodeStrings(resourceIDs)}
	if principal != nil {
		record.ClientID = principal.ClientID
		record.TenantID = principal.TenantID
		record.UserID = principal.UserID
		record.ScopesJSON = encodeStrings(principal.Scopes)
		record.KnowledgeBaseIDsJSON = encodeStrings(principal.KnowledgeBaseIDs)
	}
	_ = s.db.WithContext(ctx).Create(record).Error
}

func (s *Service) CleanupExpired(ctx context.Context) error {
	now := s.now()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for model, condition := range map[any]string{
			&BootstrapTicket{}:   "expires_at < ?",
			&Session{}:           "absolute_expires_at < ?",
			&IdempotencyRecord{}: "expires_at < ?",
			&StreamEvent{}:       "expires_at < ?",
		} {
			if err := tx.Where(condition, now).Delete(model).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) AuthorizeKnowledgeBases(principal *Principal, requested []string) error {
	if principal == nil || len(requested) == 0 {
		return ErrForbidden
	}
	for _, id := range requested {
		if !slices.Contains(principal.KnowledgeBaseIDs, strings.TrimSpace(id)) {
			return ErrForbidden
		}
	}
	return nil
}

func (s *Service) AuthorizeFile(ctx context.Context, principal *Principal, filePath string) error {
	if principal == nil || !principal.HasScope("file:read") || strings.TrimSpace(filePath) == "" {
		return ErrForbidden
	}
	var knowledge types.Knowledge
	if err := s.db.WithContext(ctx).Select("knowledge_base_id").Where("tenant_id = ? AND file_path = ?", principal.TenantID, filePath).First(&knowledge).Error; err != nil {
		return ErrForbidden
	}
	return s.AuthorizeKnowledgeBases(principal, []string{knowledge.KnowledgeBaseID})
}

func (s *Service) CreateChatBinding(ctx context.Context, principal *Principal, sessionID, mode string, requested []string) error {
	if principal == nil || (mode != "selected" && mode != "all-allowed") {
		return ErrForbidden
	}
	allowed := requested
	if mode == "selected" {
		if err := s.AuthorizeKnowledgeBases(principal, requested); err != nil {
			return err
		}
	} else {
		if len(requested) != 0 {
			return ErrForbidden
		}
		allowed = slices.Clone(principal.KnowledgeBaseIDs)
	}
	return s.db.WithContext(ctx).Create(&ChatBinding{SessionID: sessionID, ClientID: principal.ClientID, TenantID: principal.TenantID, UserID: principal.UserID, KnowledgeBaseMode: mode, AllowedKnowledgeBaseIDsJSON: encodeStrings(allowed)}).Error
}

func (s *Service) GetChatBinding(ctx context.Context, principal *Principal, sessionID string) (*ChatBinding, error) {
	if principal == nil {
		return nil, ErrForbidden
	}
	var binding ChatBinding
	err := s.db.WithContext(ctx).Where("session_id = ? AND client_id = ? AND tenant_id = ? AND user_id = ?", sessionID, principal.ClientID, principal.TenantID, principal.UserID).First(&binding).Error
	if err != nil {
		return nil, ErrForbidden
	}
	return &binding, nil
}

func (s *Service) ListChatBindings(ctx context.Context, principal *Principal) ([]ChatBinding, error) {
	if principal == nil {
		return nil, ErrForbidden
	}
	var bindings []ChatBinding
	err := s.db.WithContext(ctx).
		Where("client_id = ? AND tenant_id = ? AND user_id = ?", principal.ClientID, principal.TenantID, principal.UserID).
		Order("created_at DESC").
		Limit(100).
		Find(&bindings).Error
	return bindings, err
}

func (s *Service) DeleteChatBinding(ctx context.Context, principal *Principal, sessionID string) error {
	return s.db.WithContext(ctx).
		Where("session_id = ? AND client_id = ? AND tenant_id = ? AND user_id = ?", sessionID, principal.ClientID, principal.TenantID, principal.UserID).
		Delete(&ChatBinding{}).Error
}

func (s *Service) ResolveMessageKnowledgeBases(principal *Principal, binding *ChatBinding, selected *[]string) ([]string, error) {
	if binding == nil {
		return nil, ErrForbidden
	}
	if binding.KnowledgeBaseMode == "selected" {
		if selected == nil || len(*selected) == 0 {
			return nil, ErrInvalid
		}
		allowed := decodeStrings(binding.AllowedKnowledgeBaseIDsJSON)
		for _, id := range *selected {
			if !slices.Contains(allowed, id) || !slices.Contains(principal.KnowledgeBaseIDs, id) {
				return nil, ErrForbidden
			}
		}
		return slices.Clone(*selected), nil
	}
	if selected != nil {
		return nil, ErrInvalid
	}
	allowed := decodeStrings(binding.AllowedKnowledgeBaseIDsJSON)
	result := make([]string, 0, len(allowed))
	for _, id := range allowed {
		if slices.Contains(principal.KnowledgeBaseIDs, id) {
			result = append(result, id)
		}
	}
	if len(result) == 0 {
		return nil, ErrForbidden
	}
	return result, nil
}

func (s *Service) ClaimIdempotency(ctx context.Context, principal *Principal, endpoint, key, payload, resourceID string) (string, bool, error) {
	if principal == nil || key == "" || len(key) > 128 {
		return "", false, ErrForbidden
	}
	record := IdempotencyRecord{ClientID: principal.ClientID, UserID: principal.UserID, Endpoint: endpoint, IdempotencyKey: key, RequestHash: digest(payload), ResourceID: resourceID, ExpiresAt: s.now().Add(24 * time.Hour)}
	err := s.db.WithContext(ctx).Create(&record).Error
	if err == nil {
		return resourceID, false, nil
	}
	var existing IdempotencyRecord
	if lookupErr := s.db.WithContext(ctx).Where("client_id = ? AND user_id = ? AND endpoint = ? AND idempotency_key = ?", principal.ClientID, principal.UserID, endpoint, key).First(&existing).Error; lookupErr != nil {
		return "", false, err
	}
	if existing.RequestHash != record.RequestHash {
		return "", false, ErrConflict
	}
	return existing.ResourceID, true, nil
}

func (s *Service) ReleaseIdempotency(ctx context.Context, principal *Principal, endpoint, key string) {
	if principal == nil || key == "" {
		return
	}
	_ = s.db.WithContext(ctx).Where("client_id = ? AND user_id = ? AND endpoint = ? AND idempotency_key = ?", principal.ClientID, principal.UserID, endpoint, key).Delete(&IdempotencyRecord{}).Error
}

func (s *Service) AppendStreamEvent(ctx context.Context, sessionID, messageID, eventName string, data any) (*StreamEvent, error) {
	s.eventMu.Lock()
	defer s.eventMu.Unlock()
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 5; attempt++ {
		var maxSequence int64
		if err = s.db.WithContext(ctx).Model(&StreamEvent{}).Where("session_id = ? AND message_id = ?", sessionID, messageID).Select("COALESCE(MAX(sequence), 0)").Scan(&maxSequence).Error; err != nil {
			return nil, err
		}
		sequence := maxSequence + 1
		event := &StreamEvent{EventID: fmt.Sprintf("%s-%020d", messageID, sequence), SessionID: sessionID, MessageID: messageID, Sequence: sequence, Event: eventName, DataJSON: string(dataJSON), OccurredAt: s.now(), ExpiresAt: s.now().Add(s.eventRetention)}
		if err = s.db.WithContext(ctx).Create(event).Error; err == nil {
			return event, nil
		}
		lower := strings.ToLower(err.Error())
		if !strings.Contains(lower, "unique") && !strings.Contains(lower, "locked") {
			return nil, err
		}
		time.Sleep(time.Duration(attempt+1) * time.Millisecond)
	}
	return nil, err
}

func (s *Service) ListStreamEvents(ctx context.Context, binding *ChatBinding, messageID, afterEventID string) ([]StreamEvent, bool, error) {
	query := s.db.WithContext(ctx).Where("session_id = ? AND message_id = ?", binding.SessionID, messageID)
	if afterEventID != "" {
		var cursor StreamEvent
		if err := query.Where("event_id = ?", afterEventID).First(&cursor).Error; err != nil {
			return nil, true, nil
		}
		if cursor.ExpiresAt.Before(s.now()) {
			return nil, true, nil
		}
		query = s.db.WithContext(ctx).Where("session_id = ? AND message_id = ? AND sequence > ?", binding.SessionID, messageID, cursor.Sequence)
	}
	var events []StreamEvent
	err := query.Order("sequence ASC").Find(&events).Error
	return events, false, err
}
