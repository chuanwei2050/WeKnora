-- Migration: 000117_feishu_knowledge_sync
-- Description: Knowledge-base Feishu publish config, snapshots, runs, and source-to-remote mappings
DO $$ BEGIN RAISE NOTICE '[Migration 000117] Creating feishu publish tables'; END $$;

CREATE TABLE IF NOT EXISTS feishu_publish_configs (
    id VARCHAR(36) NOT NULL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    target_id VARCHAR(36) NOT NULL,
    app_id VARCHAR(255) NOT NULL DEFAULT '',
    app_secret_cipher TEXT NOT NULL DEFAULT '',
    space_id VARCHAR(128) NOT NULL DEFAULT '',
    space_name VARCHAR(512) NOT NULL DEFAULT '',
    parent_node_token VARCHAR(128) NOT NULL DEFAULT '',
    parent_path JSONB,
    space_locked BOOLEAN NOT NULL DEFAULT FALSE,
    connection_status VARCHAR(32) NOT NULL DEFAULT 'unknown',
    last_connection_error VARCHAR(512) NOT NULL DEFAULT '',
    last_success_at TIMESTAMP NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_feishu_publish_configs_kb
    ON feishu_publish_configs (tenant_id, knowledge_base_id)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_feishu_publish_configs_tenant_id ON feishu_publish_configs (tenant_id);
CREATE INDEX IF NOT EXISTS idx_feishu_publish_configs_kb_id ON feishu_publish_configs (knowledge_base_id);
CREATE INDEX IF NOT EXISTS idx_feishu_publish_configs_target_id ON feishu_publish_configs (target_id);
CREATE INDEX IF NOT EXISTS idx_feishu_publish_configs_deleted_at ON feishu_publish_configs (deleted_at);

CREATE TABLE IF NOT EXISTS feishu_publish_snapshots (
    id VARCHAR(36) NOT NULL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    target_id VARCHAR(36) NOT NULL,
    digest VARCHAR(128) NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_feishu_publish_snapshots_digest
    ON feishu_publish_snapshots (tenant_id, knowledge_base_id, target_id, digest);
CREATE INDEX IF NOT EXISTS idx_feishu_publish_snapshots_kb
    ON feishu_publish_snapshots (tenant_id, knowledge_base_id);

CREATE TABLE IF NOT EXISTS feishu_publish_runs (
    id VARCHAR(36) NOT NULL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    target_id VARCHAR(36) NOT NULL,
    config_id VARCHAR(36) NOT NULL,
    snapshot_id VARCHAR(36) NOT NULL,
    snapshot_digest VARCHAR(128) NOT NULL,
    space_id VARCHAR(128) NOT NULL,
    parent_node_token VARCHAR(128) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'queued',
    stage VARCHAR(64) NOT NULL DEFAULT 'queued',
    config_revision JSONB,
    counts JSONB,
    item_results JSONB,
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    error_summary VARCHAR(512) NOT NULL DEFAULT '',
    feishu_home_url VARCHAR(1024) NOT NULL DEFAULT '',
    started_at TIMESTAMP NULL,
    finished_at TIMESTAMP NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_feishu_publish_runs_idempotency
    ON feishu_publish_runs (tenant_id, knowledge_base_id, target_id, snapshot_digest);
CREATE INDEX IF NOT EXISTS idx_feishu_publish_runs_active
    ON feishu_publish_runs (tenant_id, knowledge_base_id, target_id, status);
CREATE INDEX IF NOT EXISTS idx_feishu_publish_runs_kb
    ON feishu_publish_runs (tenant_id, knowledge_base_id, created_at DESC);

CREATE TABLE IF NOT EXISTS feishu_publish_mappings (
    id VARCHAR(36) NOT NULL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    target_id VARCHAR(36) NOT NULL,
    source_kind VARCHAR(32) NOT NULL,
    source_id VARCHAR(36) NOT NULL,
    space_id VARCHAR(128) NOT NULL,
    node_token VARCHAR(128) NOT NULL DEFAULT '',
    obj_token VARCHAR(128) NOT NULL DEFAULT '',
    parent_node_token VARCHAR(128) NOT NULL DEFAULT '',
    title VARCHAR(512) NOT NULL DEFAULT '',
    meta_block_ids JSONB,
    body_block_ids JSONB,
    file_block_id VARCHAR(128) NOT NULL DEFAULT '',
    media_token VARCHAR(128) NOT NULL DEFAULT '',
    source_hash VARCHAR(128) NOT NULL DEFAULT '',
    body_hash VARCHAR(128) NOT NULL DEFAULT '',
    source_version VARCHAR(128) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    last_success_run_id VARCHAR(36) NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_feishu_publish_mappings_source
    ON feishu_publish_mappings (tenant_id, knowledge_base_id, target_id, source_kind, source_id);
CREATE INDEX IF NOT EXISTS idx_feishu_publish_mappings_node
    ON feishu_publish_mappings (tenant_id, target_id, node_token);
CREATE INDEX IF NOT EXISTS idx_feishu_publish_mappings_kb
    ON feishu_publish_mappings (tenant_id, knowledge_base_id);

DO $$ BEGIN RAISE NOTICE '[Migration 000117] feishu publish tables created successfully'; END $$;
