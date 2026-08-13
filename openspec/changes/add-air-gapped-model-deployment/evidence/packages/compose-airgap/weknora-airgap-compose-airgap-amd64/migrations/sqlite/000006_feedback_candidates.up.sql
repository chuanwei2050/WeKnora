CREATE TABLE IF NOT EXISTS feedback_candidates (
    id TEXT PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    feedback_id TEXT NOT NULL,
    target TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending_review',
    payload TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (tenant_id, feedback_id)
);
CREATE INDEX IF NOT EXISTS idx_feedback_candidates_scope ON feedback_candidates(tenant_id, status, created_at);
