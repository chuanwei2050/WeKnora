CREATE TABLE IF NOT EXISTS graph_triple_candidates (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    knowledge_id VARCHAR(36) NOT NULL,
    knowledge_version_id VARCHAR(36),
    chunk_id VARCHAR(36) NOT NULL,
    model_id VARCHAR(36),
    graph_data JSONB NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    reviewer_id VARCHAR(36),
    comment TEXT,
    superseded_by VARCHAR(36),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reviewed_at TIMESTAMPTZ,
    written_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_graph_triple_scope
    ON graph_triple_candidates (tenant_id, knowledge_base_id, status);

CREATE INDEX IF NOT EXISTS idx_graph_triple_chunk
    ON graph_triple_candidates (tenant_id, chunk_id, status);
