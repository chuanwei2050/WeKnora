CREATE TABLE integration_identity_providers (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(128) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE integration_clients (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    identity_provider_id VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    secret_hash VARCHAR(64) NOT NULL,
    previous_secret_hash VARCHAR(64) NOT NULL DEFAULT '',
    scopes_json TEXT NOT NULL DEFAULT '[]',
    knowledge_base_ids_json TEXT NOT NULL DEFAULT '[]',
    allowed_origins_json TEXT NOT NULL DEFAULT '[]',
    role_mappings_json TEXT NOT NULL DEFAULT '{}',
    max_role VARCHAR(32) NOT NULL DEFAULT 'member',
    enabled BOOLEAN NOT NULL DEFAULT 1,
    expires_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_integration_clients_tenant_id ON integration_clients(tenant_id);
CREATE INDEX idx_integration_clients_identity_provider_id ON integration_clients(identity_provider_id);
CREATE INDEX idx_integration_clients_enabled ON integration_clients(enabled);
CREATE INDEX idx_integration_clients_expires_at ON integration_clients(expires_at);

CREATE TABLE integration_external_identities (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    identity_provider_id VARCHAR(64) NOT NULL,
    external_tenant_id VARCHAR(128) NOT NULL,
    external_user_id VARCHAR(128) NOT NULL,
    user_id VARCHAR(36) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    CONSTRAINT uq_integration_external_identity UNIQUE(identity_provider_id, external_tenant_id, external_user_id)
);
CREATE INDEX idx_integration_external_identities_user_id ON integration_external_identities(user_id);

CREATE TABLE integration_bootstrap_tickets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    digest VARCHAR(64) NOT NULL UNIQUE,
    jti VARCHAR(36) NOT NULL UNIQUE,
    client_id VARCHAR(64) NOT NULL,
    user_id VARCHAR(36) NOT NULL,
    origin VARCHAR(512) NOT NULL,
    knowledge_base_ids_json TEXT NOT NULL DEFAULT '[]',
    expires_at TIMESTAMP NOT NULL,
    consumed_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_integration_bootstrap_tickets_client_id ON integration_bootstrap_tickets(client_id);
CREATE INDEX idx_integration_bootstrap_tickets_user_id ON integration_bootstrap_tickets(user_id);
CREATE INDEX idx_integration_bootstrap_tickets_expires_at ON integration_bootstrap_tickets(expires_at);

CREATE TABLE integration_sessions (
    id VARCHAR(36) PRIMARY KEY,
    digest VARCHAR(64) NOT NULL UNIQUE,
    kind VARCHAR(16) NOT NULL,
    client_id VARCHAR(64) NOT NULL,
    tenant_id BIGINT NOT NULL,
    user_id VARCHAR(36) NOT NULL DEFAULT '',
    scopes_json TEXT NOT NULL DEFAULT '[]',
    knowledge_base_ids_json TEXT NOT NULL DEFAULT '[]',
    csrf_hash VARCHAR(64) NOT NULL DEFAULT '',
    expires_at TIMESTAMP NOT NULL,
    absolute_expires_at TIMESTAMP NOT NULL,
    revoked_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_integration_sessions_client_id ON integration_sessions(client_id);
CREATE INDEX idx_integration_sessions_tenant_id ON integration_sessions(tenant_id);
CREATE INDEX idx_integration_sessions_kind ON integration_sessions(kind);
CREATE INDEX idx_integration_sessions_user_id ON integration_sessions(user_id);
CREATE INDEX idx_integration_sessions_expires_at ON integration_sessions(expires_at);
CREATE INDEX idx_integration_sessions_absolute_expires_at ON integration_sessions(absolute_expires_at);

CREATE TABLE integration_audits (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    client_id VARCHAR(64) NOT NULL DEFAULT '',
    tenant_id BIGINT NOT NULL DEFAULT 0,
    user_id VARCHAR(36) NOT NULL DEFAULT '',
    scopes_json TEXT NOT NULL DEFAULT '[]',
    knowledge_base_ids_json TEXT NOT NULL DEFAULT '[]',
    resource_ids_json TEXT NOT NULL DEFAULT '[]',
    action VARCHAR(64) NOT NULL,
    outcome VARCHAR(16) NOT NULL,
    reason VARCHAR(256) NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_integration_audits_created_at ON integration_audits(created_at);
CREATE INDEX idx_integration_audits_client_id ON integration_audits(client_id);
CREATE INDEX idx_integration_audits_tenant_id ON integration_audits(tenant_id);
CREATE INDEX idx_integration_audits_user_id ON integration_audits(user_id);
CREATE INDEX idx_integration_audits_action ON integration_audits(action);

CREATE TABLE integration_chat_bindings (
    session_id VARCHAR(36) PRIMARY KEY,
    client_id VARCHAR(64) NOT NULL,
    tenant_id BIGINT NOT NULL,
    user_id VARCHAR(36) NOT NULL,
    knowledge_base_mode VARCHAR(16) NOT NULL,
    allowed_knowledge_base_ids_json TEXT NOT NULL DEFAULT '[]',
    created_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_integration_chat_bindings_subject ON integration_chat_bindings(client_id, tenant_id, user_id);

CREATE TABLE integration_idempotency_records (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    client_id VARCHAR(64) NOT NULL,
    user_id VARCHAR(36) NOT NULL,
    endpoint VARCHAR(256) NOT NULL,
    idempotency_key VARCHAR(128) NOT NULL,
    request_hash VARCHAR(64) NOT NULL,
    resource_id VARCHAR(36) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    CONSTRAINT uq_integration_idempotency UNIQUE(client_id, user_id, endpoint, idempotency_key)
);
CREATE INDEX idx_integration_idempotency_records_expires_at ON integration_idempotency_records(expires_at);

CREATE TABLE integration_stream_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id VARCHAR(64) NOT NULL UNIQUE,
    session_id VARCHAR(36) NOT NULL,
    message_id VARCHAR(36) NOT NULL,
    sequence BIGINT NOT NULL,
    event VARCHAR(32) NOT NULL,
    data_json TEXT NOT NULL,
    occurred_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    CONSTRAINT uq_integration_stream_sequence UNIQUE(session_id, message_id, sequence)
);
CREATE INDEX idx_integration_stream_events_lookup ON integration_stream_events(session_id, message_id, sequence);
CREATE INDEX idx_integration_stream_events_expires_at ON integration_stream_events(expires_at);
