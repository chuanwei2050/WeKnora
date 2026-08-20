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
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testService(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&IdentityProvider{}, &Client{}, &ExternalIdentity{}, &BootstrapTicket{}, &Session{}, &Audit{}, &ChatBinding{}, &IdempotencyRecord{}, &StreamEvent{}))
	require.NoError(t, db.Exec(`CREATE TABLE IF NOT EXISTS knowledge_bases (id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, deleted_at DATETIME)`).Error)
	return NewService(db, nil, nil)
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

func TestClientRejectsCrossTenantKnowledgeBase(t *testing.T) {
	svc := testService(t)
	actor := &types.User{Role: types.UserRolePlatformAdmin, IsActive: true}
	require.NoError(t, svc.CreateIdentityProvider(context.Background(), actor, &IdentityProvider{ID: "idp", Name: "Test IdP"}))
	require.NoError(t, svc.db.Exec(`INSERT INTO knowledge_bases (id, tenant_id) VALUES (?, ?)`, "foreign-kb", 2).Error)
	client := &Client{TenantID: 1, IdentityProviderID: "idp", Name: "host", AllowedOriginsJSON: `["https://host.example"]`, KnowledgeBaseIDsJSON: `["foreign-kb"]`, ScopesJSON: `["rag:search"]`}
	_, err := svc.CreateClient(context.Background(), actor, client, "long-enough-secret")
	require.ErrorIs(t, err, ErrForbidden)
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

	bindings, err := svc.ListChatBindings(context.Background(), owner)
	require.NoError(t, err)
	require.Len(t, bindings, 1)
	require.Equal(t, "owner-session", bindings[0].SessionID)
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
}

func TestPrincipalFromBidReviewUsesUnifiedShape(t *testing.T) {
	user := &types.User{ID: "legacy-user", TenantID: 7, BidReviewRole: string(types.UserRoleMember), KnowledgeBaseIDs: types.StringArray{"kb-1"}}
	principal := PrincipalFromBidReview(user)
	require.Equal(t, "bidreview-legacy", principal.ClientID)
	require.Equal(t, "bidreview-legacy", principal.Kind)
	require.Equal(t, []string{"kb-1"}, principal.KnowledgeBaseIDs)
	require.Nil(t, PrincipalFromBidReview(&types.User{ID: "native"}))
}
