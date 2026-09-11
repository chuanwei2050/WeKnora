-- Migration: 000044_feishu_knowledge_sync (SQLite)
-- Knowledge-base Feishu publish config, snapshots, runs, and mappings

CREATE TABLE IF NOT EXISTS feishu_publish_configs (
    id TEXT NOT NULL PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    app_id TEXT NOT NULL DEFAULT '',
    app_secret_cipher TEXT NOT NULL DEFAULT '',
    space_id TEXT NOT NULL DEFAULT '',
    space_name TEXT NOT NULL DEFAULT '',
    parent_node_token TEXT NOT NULL DEFAULT '',
    parent_path TEXT,
    space_locked INTEGER NOT NULL DEFAULT 0,
    connection_status TEXT NOT NULL DEFAULT 'unknown',
    last_connection_error TEXT NOT NULL DEFAULT '',
    last_success_at DATETIME NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_feishu_publish_configs_kb
    ON feishu_publish_configs (tenant_id, knowledge_base_id)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_feishu_publish_configs_tenant_id ON feishu_publish_configs (tenant_id);
CREATE INDEX IF NOT EXISTS idx_feishu_publish_configs_kb_id ON feishu_publish_configs (knowledge_base_id);
CREATE INDEX IF NOT EXISTS idx_feishu_publish_configs_target_id ON feishu_publish_configs (target_id);

CREATE TABLE IF NOT EXISTS feishu_publish_snapshots (
    id TEXT NOT NULL PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    digest TEXT NOT NULL,
    payload TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_feishu_publish_snapshots_digest
    ON feishu_publish_snapshots (tenant_id, knowledge_base_id, target_id, digest);
CREATE INDEX IF NOT EXISTS idx_feishu_publish_snapshots_kb
    ON feishu_publish_snapshots (tenant_id, knowledge_base_id);

CREATE TABLE IF NOT EXISTS feishu_publish_runs (
    id TEXT NOT NULL PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    config_id TEXT NOT NULL,
    snapshot_id TEXT NOT NULL,
    snapshot_digest TEXT NOT NULL,
    space_id TEXT NOT NULL,
    parent_node_token TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'queued',
    stage TEXT NOT NULL DEFAULT 'queued',
    config_revision TEXT,
    counts TEXT,
    item_results TEXT,
    error_code TEXT NOT NULL DEFAULT '',
    error_summary TEXT NOT NULL DEFAULT '',
    feishu_home_url TEXT NOT NULL DEFAULT '',
    started_at DATETIME NULL,
    finished_at DATETIME NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_feishu_publish_runs_idempotency
    ON feishu_publish_runs (tenant_id, knowledge_base_id, target_id, snapshot_digest);
CREATE INDEX IF NOT EXISTS idx_feishu_publish_runs_active
    ON feishu_publish_runs (tenant_id, knowledge_base_id, target_id, status);
CREATE INDEX IF NOT EXISTS idx_feishu_publish_runs_kb
    ON feishu_publish_runs (tenant_id, knowledge_base_id, created_at DESC);

CREATE TABLE IF NOT EXISTS feishu_publish_mappings (
    id TEXT NOT NULL PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    source_kind TEXT NOT NULL,
    source_id TEXT NOT NULL,
    space_id TEXT NOT NULL,
    node_token TEXT NOT NULL DEFAULT '',
    obj_token TEXT NOT NULL DEFAULT '',
    parent_node_token TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    meta_block_ids TEXT,
    body_block_ids TEXT,
    file_block_id TEXT NOT NULL DEFAULT '',
    media_token TEXT NOT NULL DEFAULT '',
    source_hash TEXT NOT NULL DEFAULT '',
    body_hash TEXT NOT NULL DEFAULT '',
    source_version TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    last_success_run_id TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_feishu_publish_mappings_source
    ON feishu_publish_mappings (tenant_id, knowledge_base_id, target_id, source_kind, source_id);
CREATE INDEX IF NOT EXISTS idx_feishu_publish_mappings_node
    ON feishu_publish_mappings (tenant_id, target_id, node_token);
CREATE INDEX IF NOT EXISTS idx_feishu_publish_mappings_kb
    ON feishu_publish_mappings (tenant_id, knowledge_base_id);
