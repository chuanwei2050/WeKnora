package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testService(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.User{}, &IdentityProvider{}, &Client{}, &ExternalIdentity{}, &BootstrapTicket{}, &Session{}, &Audit{}, &ChatBinding{}, &IdempotencyRecord{}, &StreamEvent{}))
	require.NoError(t, db.Exec(`CREATE TABLE IF NOT EXISTS knowledge_bases (id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, deleted_at DATETIME)`).Error)
	return NewService(db, databaseUserService{db: db}, nil)
}

type databaseUserService struct {
	interfaces.UserService
	db *gorm.DB
}

type staticTenantService struct {
	interfaces.TenantService
	tenant *types.Tenant
}

func (service staticTenantService) GetTenantByID(_ context.Context, id uint64) (*types.Tenant, error) {
	if service.tenant == nil || service.tenant.ID != id {
		return nil, gorm.ErrRecordNotFound
	}
	return service.tenant, nil
}

func (service databaseUserService) GetUserByID(ctx context.Context, id string) (*types.User, error) {
	var user types.User
	if err := service.db.WithContext(ctx).First(&user, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (service databaseUserService) UpdateUser(ctx context.Context, user *types.User) error {
	return service.db.WithContext(ctx).Save(user).Error
}

func TestExternalIdentityIsProjectScopedAndSupportsDeactivation(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	firstClient := &Client{ID: "project-a", TenantID: 1, IdentityProviderID: "shared-idp", KnowledgeBaseIDsJSON: `[]`, RoleMappingsJSON: `{}`}
	secondClient := &Client{ID: "project-b", TenantID: 1, IdentityProviderID: "shared-idp", KnowledgeBaseIDsJSON: `[]`, RoleMappingsJSON: `{}`}
	req := BootstrapRequest{ExternalTenantID: "external-tenant", ExternalUserID: "same-user"}

	firstUser, err := svc.resolveExternalUser(ctx, firstClient, req)
	require.NoError(t, err)
	secondUser, err := svc.resolveExternalUser(ctx, secondClient, req)
	require.NoError(t, err)
	require.NotEqual(t, firstUser.ID, secondUser.ID)

	require.NoError(t, svc.db.Create(&Session{ID: "browser-session", Digest: digest("browser-token"), Kind: "browser", ClientID: firstClient.ID, TenantID: 1, UserID: firstUser.ID, ScopesJSON: `[]`, KnowledgeBaseIDsJSON: `[]`, ExpiresAt: time.Now().Add(time.Hour), AbsoluteExpiresAt: time.Now().Add(time.Hour)}).Error)
	inactive := false
	_, err = svc.resolveExternalUser(ctx, firstClient, BootstrapRequest{ExternalTenantID: req.ExternalTenantID, ExternalUserID: req.ExternalUserID, Active: &inactive})
	require.ErrorIs(t, err, ErrForbidden)
	var identity ExternalIdentity
	require.NoError(t, svc.db.First(&identity, "client_id = ? AND external_user_id = ?", firstClient.ID, req.ExternalUserID).Error)
	require.False(t, identity.Active)
	var session Session
	require.NoError(t, svc.db.First(&session, "id = ?", "browser-session").Error)
	require.NotNil(t, session.RevokedAt)

	active := true
	restoredUser, err := svc.resolveExternalUser(ctx, firstClient, BootstrapRequest{ExternalTenantID: req.ExternalTenantID, ExternalUserID: req.ExternalUserID, Active: &active})
	require.NoError(t, err)
	require.Equal(t, firstUser.ID, restoredUser.ID)
}

func TestTenantAdministratorMustBeExplicitlyBound(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	actor := &types.User{Role: types.UserRolePlatformAdmin, IsActive: true}
	require.NoError(t, svc.CreateIdentityProvider(ctx, actor, &IdentityProvider{ID: "idp", Name: "Test IdP"}))
	administrator := &types.User{ID: "tenant-admin", Username: "tenant-admin", Email: "tenant-admin@example.test", PasswordHash: "unused", TenantID: 1, IsActive: true, Role: types.UserRoleTenantAdmin}
	require.NoError(t, svc.db.Create(administrator).Error)

	client := &Client{ID: "admin-project", TenantID: 1, IdentityProviderID: "idp", Name: "host", AllowedOriginsJSON: `["https://host.example"]`, ScopesJSON: `[]`, KnowledgeBaseIDsJSON: `[]`, RoleMappingsJSON: `{"external_admin":"tenant_admin"}`, MaxRole: string(types.UserRoleTenantAdmin)}
	_, err := svc.CreateClient(ctx, actor, client, "long-enough-secret")
	require.ErrorIs(t, err, ErrInvalid)

	client.AdministratorUserID = administrator.ID
	_, err = svc.CreateClient(ctx, actor, client, "long-enough-secret")
	require.NoError(t, err)
	resolved, err := svc.resolveExternalUser(ctx, client, BootstrapRequest{ExternalTenantID: "external-tenant", ExternalUserID: "external-admin", ExternalRoles: []string{"external_admin"}})
	require.NoError(t, err)
	require.Equal(t, administrator.ID, resolved.ID)
	var users int64
	require.NoError(t, svc.db.Model(&types.User{}).Count(&users).Error)
	require.Equal(t, int64(1), users)

	secondAdministrator := &types.User{ID: "tenant-admin-2", Username: "tenant-admin-2", Email: "tenant-admin-2@example.test", PasswordHash: "unused", TenantID: 1, IsActive: true, Role: types.UserRoleTenantAdmin}
	require.NoError(t, svc.db.Create(secondAdministrator).Error)
	require.NoError(t, svc.BindClientAdministrator(ctx, actor, client.ID, secondAdministrator.ID))
	var updated Client
	require.NoError(t, svc.db.First(&updated, "id = ?", client.ID).Error)
	require.Equal(t, secondAdministrator.ID, updated.AdministratorUserID)
}

func TestWeKnoraSideTenantAdminPromotionHonoredForHostMember(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	user := &types.User{ID: "promoted-member", Username: "promoted-member", Email: "promoted@example.test", PasswordHash: "unused", TenantID: 1, IsActive: true, Role: types.UserRoleMember}
	require.NoError(t, svc.db.Create(user).Error)
	client := &Client{
		ID: "project", TenantID: 1, IdentityProviderID: "idp", Enabled: true,
		KnowledgeBaseIDsJSON: `[]`, RoleMappingsJSON: `{"member":"member"}`,
		MaxRole: string(types.UserRoleTenantAdmin), AdministratorUserID: "other-admin",
	}
	require.NoError(t, svc.db.Create(client).Error)
	require.NoError(t, svc.db.Create(&ExternalIdentity{ClientID: client.ID, IdentityProviderID: "idp", ExternalTenantID: "external", ExternalUserID: "host-user", UserID: user.ID, Active: true}).Error)

	req := BootstrapRequest{ExternalTenantID: "external", ExternalUserID: "host-user", ExternalRoles: []string{"member"}}
	resolved, err := svc.resolveExternalUser(ctx, client, req)
	require.NoError(t, err)
	require.Equal(t, types.UserRoleMember, resolved.EffectiveRole())

	user.Role = types.UserRoleTenantAdmin
	require.NoError(t, svc.db.Save(user).Error)
	resolved, err = svc.resolveExternalUser(ctx, client, req)
	require.NoError(t, err)
	require.Equal(t, user.ID, resolved.ID)
	require.Equal(t, types.UserRoleTenantAdmin, resolved.EffectiveRole())

	user.Role = types.UserRoleMember
	require.NoError(t, svc.db.Save(user).Error)
	resolved, err = svc.resolveExternalUser(ctx, client, req)
	require.NoError(t, err)
	require.Equal(t, types.UserRoleMember, resolved.EffectiveRole())
}

func TestWeKnoraSideTenantAdminPromotionIgnoresClientMaxRole(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	user := &types.User{ID: "promoted-member", Username: "promoted-member", Email: "promoted@example.test", PasswordHash: "unused", TenantID: 1, IsActive: true, Role: types.UserRoleTenantAdmin}
	require.NoError(t, svc.db.Create(user).Error)
	client := &Client{
		ID: "project", TenantID: 1, IdentityProviderID: "idp", Enabled: true,
		KnowledgeBaseIDsJSON: `[]`, RoleMappingsJSON: `{"member":"member"}`,
		MaxRole: string(types.UserRoleMember),
	}
	require.NoError(t, svc.db.Create(client).Error)
	require.NoError(t, svc.db.Create(&ExternalIdentity{ClientID: client.ID, IdentityProviderID: "idp", ExternalTenantID: "external", ExternalUserID: "host-user", UserID: user.ID, Active: true}).Error)

	resolved, err := svc.resolveExternalUser(ctx, client, BootstrapRequest{ExternalTenantID: "external", ExternalUserID: "host-user", ExternalRoles: []string{"member"}})
	require.NoError(t, err)
	require.Equal(t, user.ID, resolved.ID)
	require.Equal(t, types.UserRoleTenantAdmin, resolved.EffectiveRole())
}

func TestHostMappedTenantAdminMustHitBoundAdministrator(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	boundAdmin := &types.User{ID: "bound-admin", Username: "bound-admin", Email: "bound@example.test", PasswordHash: "unused", TenantID: 1, IsActive: true, Role: types.UserRoleTenantAdmin}
	otherAdmin := &types.User{ID: "other-admin", Username: "other-admin", Email: "other@example.test", PasswordHash: "unused", TenantID: 1, IsActive: true, Role: types.UserRoleTenantAdmin}
	require.NoError(t, svc.db.Create(boundAdmin).Error)
	require.NoError(t, svc.db.Create(otherAdmin).Error)
	client := &Client{
		ID: "project", TenantID: 1, IdentityProviderID: "idp", Enabled: true,
		KnowledgeBaseIDsJSON: `[]`, RoleMappingsJSON: `{"external_admin":"tenant_admin","member":"member"}`,
		MaxRole: string(types.UserRoleTenantAdmin), AdministratorUserID: boundAdmin.ID,
	}
	require.NoError(t, svc.db.Create(client).Error)
	require.NoError(t, svc.db.Create(&ExternalIdentity{ClientID: client.ID, IdentityProviderID: "idp", ExternalTenantID: "external", ExternalUserID: "other-admin-host", UserID: otherAdmin.ID, Active: true}).Error)
	require.NoError(t, svc.db.Create(&ExternalIdentity{ClientID: client.ID, IdentityProviderID: "idp", ExternalTenantID: "external", ExternalUserID: "bound-admin-host", UserID: boundAdmin.ID, Active: true}).Error)

	_, err := svc.resolveExternalUser(ctx, client, BootstrapRequest{ExternalTenantID: "external", ExternalUserID: "other-admin-host", ExternalRoles: []string{"external_admin"}})
	require.ErrorIs(t, err, ErrForbidden)

	resolved, err := svc.resolveExternalUser(ctx, client, BootstrapRequest{ExternalTenantID: "external", ExternalUserID: "bound-admin-host", ExternalRoles: []string{"external_admin"}})
	require.NoError(t, err)
	require.Equal(t, boundAdmin.ID, resolved.ID)
}

func TestBrowserTicketExchangeRefreshAndLogoutLifecycle(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	user := &types.User{ID: "member", Username: "member", Email: "member@example.test", PasswordHash: "unused", TenantID: 1, IsActive: true, Role: types.UserRoleMember}
	require.NoError(t, svc.db.Create(user).Error)
	client := &Client{ID: "project", TenantID: 1, IdentityProviderID: "idp", Enabled: true, ScopesJSON: `["kb:list"]`, KnowledgeBaseIDsJSON: `[]`, AllowedOriginsJSON: `["https://host.example"]`, RoleMappingsJSON: `{}`, MaxRole: string(types.UserRoleMember)}
	require.NoError(t, svc.db.Create(client).Error)
	require.NoError(t, svc.db.Create(&ExternalIdentity{ClientID: client.ID, IdentityProviderID: "idp", ExternalTenantID: "external", ExternalUserID: "member", UserID: user.ID, Active: true}).Error)
	ticket := "ticket"
	require.NoError(t, svc.db.Create(&BootstrapTicket{Digest: digest(ticket), JTI: "jti", ClientID: client.ID, UserID: user.ID, Origin: "https://host.example", KnowledgeBaseIDsJSON: `[]`, ExpiresAt: time.Now().Add(time.Minute)}).Error)

	browserToken, csrf, principal, exchangedUser, err := svc.Exchange(ctx, ticket, "https://host.example")
	require.NoError(t, err)
	require.Equal(t, user.ID, exchangedUser.ID)
	require.Equal(t, client.ID, principal.ClientID)
	_, _, _, _, err = svc.Exchange(ctx, ticket, "https://host.example")
	require.ErrorIs(t, err, ErrConflict)

	svc.tenants = staticTenantService{tenant: &types.Tenant{ID: 1, Status: string(types.TenantStatusActive)}}
	require.NoError(t, svc.ValidateCSRF(ctx, browserToken, csrf))
	require.ErrorIs(t, svc.ValidateCSRF(ctx, browserToken, ""), ErrForbidden)
	refreshedCSRF, refreshedUser, err := svc.Refresh(ctx, browserToken, csrf)
	require.NoError(t, err)
	require.Equal(t, csrf, refreshedCSRF)
	require.NotNil(t, refreshedUser)
	require.Equal(t, user.ID, refreshedUser.ID)
	require.NoError(t, svc.Logout(ctx, browserToken))
	_, _, _, err = svc.Authenticate(ctx, browserToken, "browser")
	require.ErrorIs(t, err, ErrUnauthorized)
}

func TestExpiredTicketAndUndeclaredRoleAreRejected(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	require.NoError(t, svc.db.Create(&BootstrapTicket{Digest: digest("expired"), JTI: "expired-jti", ClientID: "project", UserID: "member", Origin: "https://host.example", KnowledgeBaseIDsJSON: `[]`, ExpiresAt: time.Now().Add(-time.Second)}).Error)
	_, err := svc.consumeTicket(ctx, "expired", "https://host.example")
	require.ErrorIs(t, err, ErrUnauthorized)
	_, err = svc.resolveExternalUser(ctx, &Client{ID: "project", TenantID: 1, IdentityProviderID: "idp", RoleMappingsJSON: `{"known":"member"}`, KnowledgeBaseIDsJSON: `[]`}, BootstrapRequest{ExternalTenantID: "external", ExternalUserID: "member", ExternalRoles: []string{"unknown"}})
	require.ErrorIs(t, err, ErrForbidden)
}

func TestChatBindingExposesAllowedKnowledgeBaseIDs(t *testing.T) {
	binding := ChatBinding{AllowedKnowledgeBaseIDsJSON: `["kb-1","kb-2"]`}
	allowed := binding.AllowedKnowledgeBaseIDs()
	require.Equal(t, []string{"kb-1", "kb-2"}, allowed)
	allowed[0] = "changed"
	require.Equal(t, []string{"kb-1", "kb-2"}, binding.AllowedKnowledgeBaseIDs())
}

func TestServiceTokenRotationAndRevocation(t *testing.T) {
	svc := testService(t)
	actor := &types.User{Role: types.UserRolePlatformAdmin, IsActive: true}
	require.NoError(t, svc.CreateIdentityProvider(context.Background(), actor, &IdentityProvider{ID: "idp", Name: "Test IdP"}))
	require.NoError(t, svc.db.Exec(`INSERT INTO knowledge_bases (id, tenant_id) VALUES (?, ?)`, "kb-1", 1).Error)
	client := &Client{TenantID: 1, IdentityProviderID: "idp", Name: "host", AllowedOriginsJSON: `["https://host.example"]`, KnowledgeBaseIDsJSON: `["kb-1"]`, ScopesJSON: `["rag:search"]`}
	secret, err := svc.CreateClient(context.Background(), actor, client, "first-secret")
	require.NoError(t, err)
	require.Equal(t, "first-secret", secret)
	_, _, err = svc.IssueServiceToken(context.Background(), client.ID, secret)
	require.NoError(t, err)
	second, err := svc.RotateSecret(context.Background(), actor, client.ID)
	require.NoError(t, err)
	_, _, err = svc.IssueServiceToken(context.Background(), client.ID, secret)
	require.NoError(t, err)
	_, _, err = svc.IssueServiceToken(context.Background(), client.ID, second)
	require.NoError(t, err)
	require.NoError(t, svc.RevokePreviousSecret(context.Background(), actor, client.ID))
	_, _, err = svc.IssueServiceToken(context.Background(), client.ID, secret)
	require.ErrorIs(t, err, ErrUnauthorized)
	_, _, err = svc.IssueServiceToken(context.Background(), client.ID, second)
	require.NoError(t, err)
	var revoked int64
	require.NoError(t, svc.db.Model(&Session{}).Where("client_id = ? AND revoked_at IS NOT NULL", client.ID).Count(&revoked).Error)
	require.GreaterOrEqual(t, revoked, int64(3))
	require.NoError(t, svc.SetClientEnabled(context.Background(), actor, client.ID, false))
	_, _, err = svc.IssueServiceToken(context.Background(), client.ID, second)
	require.ErrorIs(t, err, ErrUnauthorized)
}

func TestKnowledgeWriteServiceTokenUsesBoundTenantAdministrator(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	actor := &types.User{Role: types.UserRolePlatformAdmin, IsActive: true}
	administrator := &types.User{
		ID: "tenant-admin", TenantID: 1, Username: "tenant-admin", Email: "tenant-admin@example.com",
		Role: types.UserRoleTenantAdmin, IsActive: true,
	}
	require.NoError(t, svc.db.Create(administrator).Error)
	require.NoError(t, svc.CreateIdentityProvider(ctx, actor, &IdentityProvider{ID: "idp-write", Name: "Write IdP"}))
	client := &Client{
		TenantID: 1, IdentityProviderID: "idp-write", Name: "write-client",
		AllowedOriginsJSON: `["https://host.example"]`, KnowledgeBaseIDsJSON: `[]`,
		ScopesJSON: `["knowledge:write"]`, RoleMappingsJSON: `{}`,
		AdministratorUserID: administrator.ID,
	}
	secret, err := svc.CreateClient(ctx, actor, client, "long-enough-secret")
	require.NoError(t, err)
	token, _, err := svc.IssueServiceToken(ctx, client.ID, secret)
	require.NoError(t, err)
	var session Session
	require.NoError(t, svc.db.First(&session, "digest = ?", digest(token)).Error)
	require.Equal(t, administrator.ID, session.UserID)
}

func TestRevealClientSecretLikeModelAPIKey(t *testing.T) {
	svc := testService(t)
	actor := &types.User{Role: types.UserRolePlatformAdmin, IsActive: true}
	require.NoError(t, svc.CreateIdentityProvider(context.Background(), actor, &IdentityProvider{ID: "idp", Name: "Test IdP"}))
	client := &Client{TenantID: 1, IdentityProviderID: "idp", Name: "host", AllowedOriginsJSON: `["https://host.example"]`, KnowledgeBaseIDsJSON: `[]`, ScopesJSON: `["kb:list"]`, RoleMappingsJSON: `{}`}
	secret, err := svc.CreateClient(context.Background(), actor, client, "copyable-secret")
	require.NoError(t, err)

	revealed, err := svc.RevealClientSecret(context.Background(), actor, client.ID)
	require.NoError(t, err)
	require.Equal(t, secret, revealed)

	rotated, err := svc.RotateSecret(context.Background(), actor, client.ID)
	require.NoError(t, err)
	revealed, err = svc.RevealClientSecret(context.Background(), actor, client.ID)
	require.NoError(t, err)
	require.Equal(t, rotated, revealed)
	require.NotEqual(t, secret, revealed)
}

func TestClientRejectsCrossTenantKnowledgeBase(t *testing.T) {
	svc := testService(t)
	actor := &types.User{Role: types.UserRolePlatformAdmin, IsActive: true}
	require.NoError(t, svc.CreateIdentityProvider(context.Background(), actor, &IdentityProvider{ID: "idp", Name: "Test IdP"}))
	require.NoError(t, svc.db.Exec(`INSERT INTO knowledge_bases (id, tenant_id) VALUES (?, ?)`, "foreign-kb", 2).Error)
	client := &Client{TenantID: 1, IdentityProviderID: "idp", Name: "host", AllowedOriginsJSON: `["https://host.example"]`, KnowledgeBaseIDsJSON: `["foreign-kb"]`, ScopesJSON: `["rag:search"]`}
	_, err := svc.CreateClient(context.Background(), actor, client, "long-enough-secret")
	require.ErrorIs(t, err, ErrForbidden)
}

func TestAllKnowledgeBaseModeResolvesCurrentTenantKnowledgeBases(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	actor := &types.User{Role: types.UserRolePlatformAdmin, IsActive: true}
	require.NoError(t, svc.CreateIdentityProvider(ctx, actor, &IdentityProvider{ID: "idp-all", Name: "All KB IdP"}))
	require.NoError(t, svc.db.Exec(`INSERT INTO knowledge_bases (id, tenant_id) VALUES (?, ?), (?, ?)`, "tenant-kb", 1, "foreign-kb", 2).Error)
	client := &Client{TenantID: 1, IdentityProviderID: "idp-all", Name: "all-kb-client", AllowedOriginsJSON: `["https://host.example"]`, KnowledgeBaseAccessMode: KnowledgeBaseAccessAll, KnowledgeBaseIDsJSON: `[]`, ScopesJSON: `["kb:list"]`}
	secret, err := svc.CreateClient(ctx, actor, client, "long-enough-secret")
	require.NoError(t, err)
	svc.tenants = staticTenantService{tenant: &types.Tenant{ID: 1, Status: string(types.TenantStatusActive)}}

	token, _, err := svc.IssueServiceToken(ctx, client.ID, secret)
	require.NoError(t, err)
	principal, _, _, err := svc.Authenticate(ctx, token, "service")
	require.NoError(t, err)
	require.Equal(t, []string{"tenant-kb"}, principal.KnowledgeBaseIDs)

	require.NoError(t, svc.db.Exec(`INSERT INTO knowledge_bases (id, tenant_id) VALUES (?, ?)`, "later-kb", 1).Error)
	refreshedPrincipal, _, _, err := svc.Authenticate(ctx, token, "service")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"tenant-kb", "later-kb"}, refreshedPrincipal.KnowledgeBaseIDs)

	newToken, _, err := svc.IssueServiceToken(ctx, client.ID, secret)
	require.NoError(t, err)
	newPrincipal, _, _, err := svc.Authenticate(ctx, newToken, "service")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"tenant-kb", "later-kb"}, newPrincipal.KnowledgeBaseIDs)
}

func TestConsumeTicketIsSingleUseAndOriginBound(t *testing.T) {
	svc := testService(t)
	token := "one-time-ticket"
	require.NoError(t, svc.db.Create(&BootstrapTicket{Digest: digest(token), JTI: "jti", ClientID: "client", UserID: "user", Origin: "https://host.example", KnowledgeBaseIDsJSON: `[]`, ExpiresAt: time.Now().Add(time.Minute)}).Error)
	_, err := svc.consumeTicket(context.Background(), token, "https://wrong.example")
	require.ErrorIs(t, err, ErrUnauthorized)

	var successes int
	var conflicts int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, consumeErr := svc.consumeTicket(context.Background(), token, "https://host.example")
			mu.Lock()
			defer mu.Unlock()
			if consumeErr == nil {
				successes++
			} else if errors.Is(consumeErr, ErrConflict) {
				conflicts++
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
}

func TestKnowledgeSelectionAndIdempotency(t *testing.T) {
	svc := testService(t)
	principal := &Principal{ClientID: "client", TenantID: 1, UserID: "user", KnowledgeBaseIDs: []string{"kb-1", "kb-2"}}
	require.NoError(t, svc.CreateChatBinding(context.Background(), principal, "session", "selected", []string{"kb-1", "kb-2"}))
	binding, err := svc.GetChatBinding(context.Background(), principal, "session")
	require.NoError(t, err)
	selection := []string{"kb-1"}
	resolved, err := svc.ResolveMessageKnowledgeBases(principal, binding, &selection)
	require.NoError(t, err)
	require.Equal(t, selection, resolved)
	denied := []string{"kb-3"}
	_, err = svc.ResolveMessageKnowledgeBases(principal, binding, &denied)
	require.ErrorIs(t, err, ErrForbidden)

	resource, replay, err := svc.ClaimIdempotency(context.Background(), principal, "/messages", "key", "payload", "message")
	require.NoError(t, err)
	require.False(t, replay)
	require.Equal(t, "message", resource)
	resource, replay, err = svc.ClaimIdempotency(context.Background(), principal, "/messages", "key", "payload", "other")
	require.NoError(t, err)
	require.True(t, replay)
	require.Equal(t, "message", resource)
	_, _, err = svc.ClaimIdempotency(context.Background(), principal, "/messages", "key", "different", "other")
	require.ErrorIs(t, err, ErrConflict)
}

func TestExpiredEventCursorReturnsGone(t *testing.T) {
	svc := testService(t)
	binding := &ChatBinding{SessionID: "session"}
	event := &StreamEvent{EventID: "cursor", SessionID: "session", MessageID: "message", Sequence: 1, Event: "answer.delta", DataJSON: `{}`, OccurredAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(-time.Minute)}
	require.NoError(t, svc.db.Create(event).Error)
	_, gone, err := svc.ListStreamEvents(context.Background(), binding, "message", "cursor")
	require.NoError(t, err)
	require.True(t, gone)
}

func TestExpiredClientWrongCredentialKindAndOriginAreRejected(t *testing.T) {
	svc := testService(t)
	actor := &types.User{Role: types.UserRolePlatformAdmin, IsActive: true}
	require.NoError(t, svc.CreateIdentityProvider(context.Background(), actor, &IdentityProvider{ID: "idp", Name: "Test IdP"}))
	expired := time.Now().Add(-time.Minute)
	client := &Client{ID: "expired", TenantID: 1, IdentityProviderID: "idp", Name: "host", AllowedOriginsJSON: `["https://host.example"]`, ScopesJSON: `["kb:list"]`, ExpiresAt: &expired}
	secret, err := svc.CreateClient(context.Background(), actor, client, "long-enough-secret")
	require.NoError(t, err)
	_, _, err = svc.IssueServiceToken(context.Background(), client.ID, secret)
	require.ErrorIs(t, err, ErrUnauthorized)
	require.False(t, svc.IsAllowedOrigin(context.Background(), "https://host.example"))

	require.NoError(t, svc.db.Create(&Session{ID: "browser", Digest: digest("browser-token"), Kind: "browser", ClientID: client.ID, TenantID: 1, ScopesJSON: `[]`, KnowledgeBaseIDsJSON: `[]`, ExpiresAt: time.Now().Add(time.Minute), AbsoluteExpiresAt: time.Now().Add(time.Hour)}).Error)
	_, _, _, err = svc.Authenticate(context.Background(), "browser-token", "service")
	require.ErrorIs(t, err, ErrUnauthorized)
	require.ErrorIs(t, svc.ValidateCSRF(context.Background(), "browser-token", "wrong"), ErrForbidden)
}

func TestValidateCSRFAcceptsActiveSameSubjectBrowserSession(t *testing.T) {
	svc := testService(t)
	now := svc.now()
	base := Session{Kind: "browser", ClientID: "client", TenantID: 1, UserID: "user", ScopesJSON: `[]`, KnowledgeBaseIDsJSON: `[]`, ExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(2 * time.Hour)}
	current := base
	current.ID = "current"
	current.Digest = digest("current-token")
	current.CSRFHash = digest("current-csrf")
	peer := base
	peer.ID = "peer"
	peer.Digest = digest("peer-token")
	peer.CSRFHash = digest("peer-csrf")
	otherUser := base
	otherUser.ID = "other-user"
	otherUser.Digest = digest("other-user-token")
	otherUser.CSRFHash = digest("other-user-csrf")
	otherUser.UserID = "other"
	otherClient := base
	otherClient.ID = "other-client"
	otherClient.Digest = digest("other-client-token")
	otherClient.CSRFHash = digest("other-client-csrf")
	otherClient.ClientID = "other-client"
	otherTenant := base
	otherTenant.ID = "other-tenant"
	otherTenant.Digest = digest("other-tenant-token")
	otherTenant.CSRFHash = digest("other-tenant-csrf")
	otherTenant.TenantID = 2
	expired := base
	expired.ID = "expired-peer"
	expired.Digest = digest("expired-token")
	expired.CSRFHash = digest("expired-csrf")
	expired.ExpiresAt = now.Add(-time.Minute)
	require.NoError(t, svc.db.Create(&[]Session{current, peer, otherUser, otherClient, otherTenant, expired}).Error)

	require.NoError(t, svc.ValidateCSRF(context.Background(), "current-token", "current-csrf"))
	require.NoError(t, svc.ValidateCSRF(context.Background(), "current-token", "peer-csrf"))
	require.ErrorIs(t, svc.ValidateCSRF(context.Background(), "current-token", "other-user-csrf"), ErrForbidden)
	require.ErrorIs(t, svc.ValidateCSRF(context.Background(), "current-token", "other-client-csrf"), ErrForbidden)
	require.ErrorIs(t, svc.ValidateCSRF(context.Background(), "current-token", "other-tenant-csrf"), ErrForbidden)
	require.ErrorIs(t, svc.ValidateCSRF(context.Background(), "current-token", "expired-csrf"), ErrForbidden)
}

func TestChatBindingRejectsOtherSubjectAndModeConflicts(t *testing.T) {
	svc := testService(t)
	owner := &Principal{ClientID: "client", TenantID: 1, UserID: "owner", KnowledgeBaseIDs: []string{"kb-1"}}
	require.NoError(t, svc.CreateChatBinding(context.Background(), owner, "session", "selected", []string{"kb-1"}))
	for _, other := range []*Principal{
		{ClientID: "other", TenantID: 1, UserID: "owner"},
		{ClientID: "client", TenantID: 2, UserID: "owner"},
		{ClientID: "client", TenantID: 1, UserID: "other"},
	} {
		_, err := svc.GetChatBinding(context.Background(), other, "session")
		require.ErrorIs(t, err, ErrForbidden)
	}
	binding, err := svc.GetChatBinding(context.Background(), owner, "session")
	require.NoError(t, err)
	_, err = svc.ResolveMessageKnowledgeBases(owner, binding, nil)
	require.ErrorIs(t, err, ErrInvalid)

	require.NoError(t, svc.CreateChatBinding(context.Background(), owner, "all", "all-allowed", nil))
	allBinding, err := svc.GetChatBinding(context.Background(), owner, "all")
	require.NoError(t, err)
	selected := []string{"kb-1"}
	_, err = svc.ResolveMessageKnowledgeBases(owner, allBinding, &selected)
	require.ErrorIs(t, err, ErrInvalid)
}

func TestListChatBindingsOnlyReturnsCurrentSubject(t *testing.T) {
	svc := testService(t)
	owner := &Principal{ClientID: "client", TenantID: 1, UserID: "owner", KnowledgeBaseIDs: []string{"kb-1"}}
	other := &Principal{ClientID: "client", TenantID: 1, UserID: "other", KnowledgeBaseIDs: []string{"kb-1"}}
	other.UserID = "other-user"
	require.NoError(t, svc.CreateChatBinding(context.Background(), owner, "owner-session", "selected", []string{"kb-1"}))
	require.NoError(t, svc.CreateChatBinding(context.Background(), other, "other-session", "selected", []string{"kb-1"}))
	require.NoError(t, svc.db.Create(&ChatBinding{SessionID: "non-widget-session", ClientID: owner.ClientID, TenantID: owner.TenantID, UserID: owner.UserID, Source: "", KnowledgeBaseMode: "selected", AllowedKnowledgeBaseIDsJSON: `["kb-1"]`}).Error)

	bindings, err := svc.ListChatBindings(context.Background(), owner)
	require.NoError(t, err)
	require.Len(t, bindings, 1)
	require.Equal(t, "owner-session", bindings[0].SessionID)
	require.Equal(t, "widget", bindings[0].Source)
}

func TestDeleteChatBindingOnlyDeletesCurrentSubject(t *testing.T) {
	svc := testService(t)
	owner := &Principal{ClientID: "client", TenantID: 1, UserID: "owner", KnowledgeBaseIDs: []string{"kb-1"}}
	other := &Principal{ClientID: "client", TenantID: 1, UserID: "other", KnowledgeBaseIDs: []string{"kb-1"}}
	require.NoError(t, svc.CreateChatBinding(context.Background(), owner, "session", "selected", []string{"kb-1"}))

	require.NoError(t, svc.DeleteChatBinding(context.Background(), other, "session"))
	_, err := svc.GetChatBinding(context.Background(), owner, "session")
	require.NoError(t, err)

	require.NoError(t, svc.DeleteChatBinding(context.Background(), owner, "session"))
	_, err = svc.GetChatBinding(context.Background(), owner, "session")
	require.ErrorIs(t, err, ErrForbidden)
}

func TestConcurrentStreamEventsKeepMonotonicSequence(t *testing.T) {
	svc := testService(t)
	var wg sync.WaitGroup
	errorsByWorker := make(chan error, 12)
	for i := range 12 {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, err := svc.AppendStreamEvent(context.Background(), "session", "message", "answer.delta", map[string]int{"index": index})
			errorsByWorker <- err
		}(i)
	}
	wg.Wait()
	close(errorsByWorker)
	for err := range errorsByWorker {
		require.NoError(t, err)
	}
	var events []StreamEvent
	require.NoError(t, svc.db.Order("sequence ASC").Find(&events).Error)
	require.Len(t, events, 12)
	sequences := make([]int, 0, len(events))
	for _, streamEvent := range events {
		sequences = append(sequences, int(streamEvent.Sequence))
		require.Equal(t, fmt.Sprintf("message-%020d", streamEvent.Sequence), streamEvent.EventID)
	}
	sort.Ints(sequences)
	for index, sequence := range sequences {
		require.Equal(t, index+1, sequence)
	}
}

func TestSQLiteMigrationUsesIntegrationTables(t *testing.T) {
	migration, err := os.ReadFile("../../migrations/sqlite/000027_integration_clients.up.sql")
	require.NoError(t, err)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(migration)).Error)
	for _, table := range []string{
		"integration_clients", "integration_sessions", "integration_chat_bindings", "integration_stream_events",
	} {
		require.True(t, db.Migrator().HasTable(table), table)
	}
	require.False(t, db.Migrator().HasTable("sessions"))
	var eventColumn struct {
		Type string `gorm:"column:type"`
	}
	require.NoError(t, db.Raw(`SELECT type FROM pragma_table_info('integration_stream_events') WHERE name = 'event_id'`).Scan(&eventColumn).Error)
	require.Equal(t, "VARCHAR(64)", eventColumn.Type)
	var indexCount int64
	require.NoError(t, db.Raw(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name IN ('idx_integration_idempotency_records_expires_at', 'idx_integration_stream_events_expires_at')`).Scan(&indexCount).Error)
	require.Equal(t, int64(2), indexCount)
	down, err := os.ReadFile("../../migrations/sqlite/000027_integration_clients.down.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(down)).Error)
	require.False(t, db.Migrator().HasTable("integration_clients"))
	require.False(t, db.Migrator().HasTable("integration_stream_events"))
}

func TestClientBoundaryRejectsWeakSecretUnknownScopeAndRole(t *testing.T) {
	svc := testService(t)
	actor := &types.User{Role: types.UserRolePlatformAdmin, IsActive: true}
	require.NoError(t, svc.CreateIdentityProvider(context.Background(), actor, &IdentityProvider{ID: "idp", Name: "Test IdP"}))
	base := func() *Client {
		return &Client{TenantID: 1, IdentityProviderID: "idp", Name: "host", AllowedOriginsJSON: `["https://host.example"]`, KnowledgeBaseIDsJSON: `[]`, ScopesJSON: `["kb:list"]`, RoleMappingsJSON: `{}`}
	}
	_, err := svc.CreateClient(context.Background(), actor, base(), "short")
	require.ErrorIs(t, err, ErrInvalid)
	unknownScope := base()
	unknownScope.ScopesJSON = `["admin:everything"]`
	_, err = svc.CreateClient(context.Background(), actor, unknownScope, "long-enough-secret")
	require.ErrorIs(t, err, ErrForbidden)
	platformRole := base()
	platformRole.MaxRole = string(types.UserRolePlatformAdmin)
	_, err = svc.CreateClient(context.Background(), actor, platformRole, "long-enough-secret")
	require.ErrorIs(t, err, ErrForbidden)
	for _, origin := range []string{
		"ftp://host.example",
		"https://user@host.example",
		"https://host.example?redirect=other",
	} {
		invalidOrigin := base()
		invalidOrigin.AllowedOriginsJSON = encodeStrings([]string{origin})
		_, err = svc.CreateClient(context.Background(), actor, invalidOrigin, "long-enough-secret")
		require.Error(t, err, origin)
	}
}

func TestUpdateClientScopesRevokesExistingSessions(t *testing.T) {
	svc := testService(t)
	actor := &types.User{Role: types.UserRolePlatformAdmin, IsActive: true}
	require.NoError(t, svc.db.Create(&Client{ID: "client", TenantID: 1, Enabled: true, ScopesJSON: `["kb:list"]`, KnowledgeBaseIDsJSON: `[]`, AllowedOriginsJSON: `[]`, RoleMappingsJSON: `{}`}).Error)
	require.NoError(t, svc.db.Create(&Session{ID: "session", ClientID: "client", TenantID: 1, Kind: "service", Digest: digest("token"), ScopesJSON: `["kb:list"]`, KnowledgeBaseIDsJSON: `[]`, ExpiresAt: time.Now().Add(time.Hour), AbsoluteExpiresAt: time.Now().Add(time.Hour)}).Error)

	require.NoError(t, svc.UpdateClientScopes(context.Background(), actor, "client", []string{"kb:list", "table:analyze"}))
	var client Client
	require.NoError(t, svc.db.First(&client, "id = ?", "client").Error)
	require.Equal(t, []string{"kb:list", "table:analyze"}, client.Scopes())
	var session Session
	require.NoError(t, svc.db.First(&session, "id = ?", "session").Error)
	require.NotNil(t, session.RevokedAt)

	require.ErrorIs(t, svc.UpdateClientScopes(context.Background(), actor, "client", []string{"unknown"}), ErrForbidden)
}

func TestUpdateClientKnowledgeBasesValidatesTenantAndRevokesSessions(t *testing.T) {
	svc := testService(t)
	actor := &types.User{Role: types.UserRolePlatformAdmin, IsActive: true}
	require.NoError(t, svc.db.Exec("INSERT INTO knowledge_bases (id, tenant_id) VALUES (?, ?), (?, ?)", "kb-1", 1, "foreign-kb", 2).Error)
	require.NoError(t, svc.db.Create(&Client{ID: "client", TenantID: 1, Enabled: true, ScopesJSON: `[]`, KnowledgeBaseIDsJSON: `[]`, AllowedOriginsJSON: `[]`, RoleMappingsJSON: `{}`}).Error)
	require.NoError(t, svc.db.Create(&Session{ID: "session", ClientID: "client", TenantID: 1, Kind: "service", Digest: digest("token"), ScopesJSON: `[]`, KnowledgeBaseIDsJSON: `[]`, ExpiresAt: time.Now().Add(time.Hour), AbsoluteExpiresAt: time.Now().Add(time.Hour)}).Error)

	require.NoError(t, svc.UpdateClientKnowledgeBases(context.Background(), actor, "client", []string{"kb-1", "kb-1"}))
	var client Client
	require.NoError(t, svc.db.First(&client, "id = ?", "client").Error)
	require.Equal(t, []string{"kb-1"}, client.KnowledgeBaseIDs())
	var session Session
	require.NoError(t, svc.db.First(&session, "id = ?", "session").Error)
	require.NotNil(t, session.RevokedAt)

	require.ErrorIs(t, svc.UpdateClientKnowledgeBases(context.Background(), actor, "client", []string{"foreign-kb"}), ErrForbidden)
	require.ErrorIs(t, svc.UpdateClientKnowledgeBases(context.Background(), &types.User{Role: types.UserRoleTenantAdmin}, "client", []string{"kb-1"}), ErrForbidden)
}

func TestUpdateClientKnowledgeBasesRejectsSelectedIDsWhenAccessModeAll(t *testing.T) {
	svc := testService(t)
	actor := &types.User{Role: types.UserRolePlatformAdmin, IsActive: true}
	require.NoError(t, svc.db.Exec("INSERT INTO knowledge_bases (id, tenant_id) VALUES (?, ?)", "kb-1", 1).Error)
	require.NoError(t, svc.db.Create(&Client{
		ID: "all-client", TenantID: 1, Enabled: true, KnowledgeBaseAccessMode: KnowledgeBaseAccessAll,
		ScopesJSON: `[]`, KnowledgeBaseIDsJSON: `[]`, AllowedOriginsJSON: `[]`, RoleMappingsJSON: `{}`,
	}).Error)

	require.ErrorIs(t, svc.UpdateClientKnowledgeBases(context.Background(), actor, "all-client", []string{"kb-1"}), ErrInvalid)
	require.NoError(t, svc.UpdateClientKnowledgeBases(context.Background(), actor, "all-client", nil))
}

func TestPrincipalFromBidReviewUsesUnifiedShape(t *testing.T) {
	user := &types.User{ID: "legacy-user", TenantID: 7, BidReviewRole: string(types.UserRoleMember), KnowledgeBaseIDs: types.StringArray{"kb-1"}}
	principal := PrincipalFromBidReview(user)
	require.Equal(t, "bidreview-legacy", principal.ClientID)
	require.Equal(t, "bidreview-legacy", principal.Kind)
	require.Equal(t, []string{"kb-1"}, principal.KnowledgeBaseIDs)
	require.Nil(t, PrincipalFromBidReview(&types.User{ID: "native"}))
}
