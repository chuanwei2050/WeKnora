ALTER TABLE knowledge_bases ADD COLUMN governance TEXT NOT NULL DEFAULT '{}';
ALTER TABLE knowledges ADD COLUMN current_version_id TEXT;
ALTER TABLE chunks ADD COLUMN knowledge_version_id TEXT;

CREATE TABLE knowledge_versions (
    id TEXT PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    knowledge_id TEXT NOT NULL,
    version_label TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    snapshot_ref TEXT,
    source_metadata TEXT NOT NULL DEFAULT '{}',
    previous_version_id TEXT,
    status TEXT NOT NULL,
    created_by TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    effective_at DATETIME,
    expires_at DATETIME,
    UNIQUE (knowledge_id, content_hash, version_label)
);
CREATE INDEX idx_knowledge_versions_scope ON knowledge_versions(tenant_id, knowledge_id, status);
CREATE INDEX idx_chunks_knowledge_version ON chunks(knowledge_version_id);

CREATE TABLE knowledge_version_reviews (
    id TEXT PRIMARY KEY,
    version_id TEXT NOT NULL,
    reviewer_id TEXT NOT NULL,
    action TEXT NOT NULL,
    comment TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (version_id) REFERENCES knowledge_versions(id)
);
CREATE INDEX idx_knowledge_version_reviews_version ON knowledge_version_reviews(version_id, created_at);
