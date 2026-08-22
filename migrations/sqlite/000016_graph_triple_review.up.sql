CREATE TABLE IF NOT EXISTS graph_triple_candidates (
    id TEXT PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    knowledge_id TEXT NOT NULL,
    knowledge_version_id TEXT,
    chunk_id TEXT NOT NULL,
    model_id TEXT,
    graph_data TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    reviewer_id TEXT,
    comment TEXT,
    superseded_by TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    reviewed_at DATETIME,
    written_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_graph_triple_scope
    ON graph_triple_candidates (tenant_id, knowledge_base_id, status);

CREATE INDEX IF NOT EXISTS idx_graph_triple_chunk
    ON graph_triple_candidates (tenant_id, chunk_id, status);
