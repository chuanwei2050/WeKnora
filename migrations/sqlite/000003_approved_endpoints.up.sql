CREATE TABLE approved_endpoints (
    id TEXT PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    scheme TEXT NOT NULL,
    host TEXT NOT NULL,
    port INTEGER NOT NULL,
    protocol TEXT NOT NULL,
    tls_required INTEGER NOT NULL DEFAULT 0,
    category TEXT NOT NULL,
    allowed_uses TEXT NOT NULL DEFAULT '[]',
    allowed_model_roles TEXT NOT NULL DEFAULT '[]',
    created_by TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (tenant_id, scheme, host, port, category)
);
CREATE INDEX idx_approved_endpoints_scope ON approved_endpoints(tenant_id, category);
CREATE TABLE IF NOT EXISTS approved_endpoint_audits (id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, endpoint_id TEXT NOT NULL, actor_id TEXT NOT NULL, action TEXT NOT NULL, before_json TEXT, after_json TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE INDEX IF NOT EXISTS idx_approved_endpoint_audits_scope ON approved_endpoint_audits(tenant_id, endpoint_id, created_at);
