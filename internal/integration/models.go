package integration

import (
	"slices"
	"time"
)

type IdentityProvider struct {
	ID        string `gorm:"type:varchar(64);primaryKey"`
	Name      string `gorm:"type:varchar(128);not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (IdentityProvider) TableName() string { return "integration_identity_providers" }

type Client struct {
	ID                      string     `gorm:"type:varchar(64);primaryKey"`
	TenantID                uint64     `gorm:"not null;index"`
	IdentityProviderID      string     `gorm:"type:varchar(64);not null;index"`
	AdministratorUserID     string     `gorm:"type:varchar(36);not null;default:''"`
	Name                    string     `gorm:"type:varchar(128);not null"`
	SecretHash              string     `gorm:"type:varchar(64);not null"`
	PreviousSecretHash      string     `gorm:"type:varchar(64)"`
	ScopesJSON              string     `gorm:"type:text;not null"`
	KnowledgeBaseAccessMode string     `gorm:"type:varchar(16);not null;default:'selected'"`
	KnowledgeBaseIDsJSON    string     `gorm:"type:text;not null"`
	AllowedOriginsJSON      string     `gorm:"type:text;not null"`
	RoleMappingsJSON        string     `gorm:"type:text;not null"`
	MaxRole                 string     `gorm:"type:varchar(32);not null"`
	Enabled                 bool       `gorm:"not null;default:true;index"`
	ExpiresAt               *time.Time `gorm:"index"`
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

func (Client) TableName() string { return "integration_clients" }

func (c Client) Scopes() []string { return decodeStrings(c.ScopesJSON) }

func (c Client) KnowledgeBaseIDs() []string { return decodeStrings(c.KnowledgeBaseIDsJSON) }

type ExternalIdentity struct {
	ID                 uint64 `gorm:"primaryKey"`
	ClientID           string `gorm:"type:varchar(64);not null;uniqueIndex:idx_integration_identity"`
	IdentityProviderID string `gorm:"type:varchar(64);not null"`
	ExternalTenantID   string `gorm:"type:varchar(128);not null;uniqueIndex:idx_integration_identity"`
	ExternalUserID     string `gorm:"type:varchar(128);not null;uniqueIndex:idx_integration_identity"`
	UserID             string `gorm:"type:varchar(36);not null;index"`
	Active             bool   `gorm:"not null;default:true;index"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (ExternalIdentity) TableName() string { return "integration_external_identities" }

type BootstrapTicket struct {
	ID                   uint64    `gorm:"primaryKey"`
	Digest               string    `gorm:"type:varchar(64);not null;uniqueIndex"`
	JTI                  string    `gorm:"type:varchar(36);not null;uniqueIndex"`
	ClientID             string    `gorm:"type:varchar(64);not null;index"`
	UserID               string    `gorm:"type:varchar(36);not null;index"`
	Origin               string    `gorm:"type:varchar(512);not null"`
	KnowledgeBaseIDsJSON string    `gorm:"type:text;not null"`
	ExpiresAt            time.Time `gorm:"not null;index"`
	ConsumedAt           *time.Time
	CreatedAt            time.Time
}

func (BootstrapTicket) TableName() string { return "integration_bootstrap_tickets" }

type Session struct {
	ID                   string    `gorm:"type:varchar(36);primaryKey"`
	Digest               string    `gorm:"type:varchar(64);not null;uniqueIndex"`
	Kind                 string    `gorm:"type:varchar(16);not null;index"`
	ClientID             string    `gorm:"type:varchar(64);not null;index"`
	TenantID             uint64    `gorm:"not null;index"`
	UserID               string    `gorm:"type:varchar(36);index"`
	ScopesJSON           string    `gorm:"type:text;not null"`
	KnowledgeBaseIDsJSON string    `gorm:"type:text;not null"`
	CSRFHash             string    `gorm:"type:varchar(64)"`
	ExpiresAt            time.Time `gorm:"not null;index"`
	AbsoluteExpiresAt    time.Time `gorm:"not null;index"`
	RevokedAt            *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

func (Session) TableName() string { return "integration_sessions" }

type Audit struct {
	ID                   uint64    `gorm:"primaryKey"`
	ClientID             string    `gorm:"type:varchar(64);index"`
	TenantID             uint64    `gorm:"index"`
	UserID               string    `gorm:"type:varchar(36);index"`
	ScopesJSON           string    `gorm:"type:text;not null"`
	KnowledgeBaseIDsJSON string    `gorm:"type:text;not null"`
	ResourceIDsJSON      string    `gorm:"type:text;not null"`
	Action               string    `gorm:"type:varchar(64);not null;index"`
	Outcome              string    `gorm:"type:varchar(16);not null"`
	Reason               string    `gorm:"type:varchar(256)"`
	CreatedAt            time.Time `gorm:"index"`
}

func (Audit) TableName() string { return "integration_audits" }

type Principal struct {
	ClientID         string
	TenantID         uint64
	UserID           string
	Scopes           []string
	KnowledgeBaseIDs []string
	Kind             string
}

func (p *Principal) HasScope(scope string) bool {
	return p != nil && slices.Contains(p.Scopes, scope)
}

type ChatBinding struct {
	SessionID                   string `gorm:"type:varchar(36);primaryKey"`
	ClientID                    string `gorm:"type:varchar(64);not null;index:idx_integration_chat_subject"`
	TenantID                    uint64 `gorm:"not null;index:idx_integration_chat_subject"`
	UserID                      string `gorm:"type:varchar(36);not null;index:idx_integration_chat_subject"`
	KnowledgeBaseMode           string `gorm:"type:varchar(16);not null"`
	AllowedKnowledgeBaseIDsJSON string `gorm:"type:text;not null"`
	CreatedAt                   time.Time
}

func (ChatBinding) TableName() string { return "integration_chat_bindings" }

func (binding ChatBinding) AllowedKnowledgeBaseIDs() []string {
	return decodeStrings(binding.AllowedKnowledgeBaseIDsJSON)
}

type IdempotencyRecord struct {
	ID             uint64 `gorm:"primaryKey"`
	ClientID       string `gorm:"type:varchar(64);not null;uniqueIndex:idx_integration_idempotency"`
	UserID         string `gorm:"type:varchar(36);not null;uniqueIndex:idx_integration_idempotency"`
	Endpoint       string `gorm:"type:varchar(256);not null;uniqueIndex:idx_integration_idempotency"`
	IdempotencyKey string `gorm:"type:varchar(128);not null;uniqueIndex:idx_integration_idempotency"`
	RequestHash    string `gorm:"type:varchar(64);not null"`
	ResourceID     string `gorm:"type:varchar(36);not null"`
	CreatedAt      time.Time
	ExpiresAt      time.Time `gorm:"index"`
}

func (IdempotencyRecord) TableName() string { return "integration_idempotency_records" }

type StreamEvent struct {
	ID         uint64 `gorm:"primaryKey"`
	EventID    string `gorm:"type:varchar(64);not null;uniqueIndex"`
	SessionID  string `gorm:"type:varchar(36);not null;uniqueIndex:idx_integration_stream_sequence;index:idx_integration_stream_lookup"`
	MessageID  string `gorm:"type:varchar(36);not null;uniqueIndex:idx_integration_stream_sequence;index:idx_integration_stream_lookup"`
	Sequence   int64  `gorm:"not null;uniqueIndex:idx_integration_stream_sequence"`
	Event      string `gorm:"type:varchar(32);not null"`
	DataJSON   string `gorm:"type:text;not null"`
	OccurredAt time.Time
	ExpiresAt  time.Time `gorm:"index"`
}

func (StreamEvent) TableName() string { return "integration_stream_events" }
