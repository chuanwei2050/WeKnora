-- Restore endpoint approval tables when a database reached a later migration
-- version without applying the original approved-endpoint migration.
CREATE TABLE IF NOT EXISTS approved_endpoints (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    scheme VARCHAR(16) NOT NULL,
    host VARCHAR(255) NOT NULL,
    port INTEGER NOT NULL,
    protocol VARCHAR(64) NOT NULL,
    tls_required BOOLEAN NOT NULL DEFAULT FALSE,
    category VARCHAR(32) NOT NULL,
    allowed_uses JSONB NOT NULL DEFAULT '[]'::jsonb,
    allowed_model_roles JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_by VARCHAR(36) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, scheme, host, port, category)
);
CREATE INDEX IF NOT EXISTS idx_approved_endpoints_scope ON approved_endpoints(tenant_id, category);

CREATE TABLE IF NOT EXISTS approved_endpoint_audits (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    endpoint_id VARCHAR(36) NOT NULL,
    actor_id VARCHAR(36) NOT NULL,
    action VARCHAR(32) NOT NULL,
    before_json TEXT,
    after_json TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_approved_endpoint_audits_scope ON approved_endpoint_audits(tenant_id, endpoint_id, created_at);
