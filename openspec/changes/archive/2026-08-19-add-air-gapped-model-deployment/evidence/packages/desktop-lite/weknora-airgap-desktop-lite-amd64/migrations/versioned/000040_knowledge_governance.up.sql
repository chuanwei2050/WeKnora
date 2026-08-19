-- Description: Add opt-in knowledge governance, immutable versions and review history.

ALTER TABLE knowledge_bases ADD COLUMN IF NOT EXISTS governance JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE knowledges ADD COLUMN IF NOT EXISTS current_version_id VARCHAR(36);
ALTER TABLE chunks ADD COLUMN IF NOT EXISTS knowledge_version_id VARCHAR(36);

CREATE TABLE IF NOT EXISTS knowledge_versions (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_id VARCHAR(36) NOT NULL,
    version_label VARCHAR(255) NOT NULL,
    content_hash VARCHAR(128) NOT NULL,
    snapshot_ref TEXT,
    source_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    previous_version_id VARCHAR(36),
    status VARCHAR(32) NOT NULL,
    created_by VARCHAR(36) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    effective_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    UNIQUE (knowledge_id, content_hash, version_label)
);
CREATE INDEX IF NOT EXISTS idx_knowledge_versions_scope ON knowledge_versions(tenant_id, knowledge_id, status);
CREATE INDEX IF NOT EXISTS idx_knowledges_current_version ON knowledges(current_version_id);
CREATE INDEX IF NOT EXISTS idx_chunks_knowledge_version ON chunks(knowledge_version_id);

CREATE TABLE IF NOT EXISTS knowledge_version_reviews (
    id VARCHAR(36) PRIMARY KEY,
    version_id VARCHAR(36) NOT NULL REFERENCES knowledge_versions(id),
    reviewer_id VARCHAR(36) NOT NULL,
    action VARCHAR(32) NOT NULL,
    comment TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_knowledge_version_reviews_version ON knowledge_version_reviews(version_id, created_at);
