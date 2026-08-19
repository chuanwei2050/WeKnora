CREATE TABLE IF NOT EXISTS feedback_candidates (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    feedback_id VARCHAR(36) NOT NULL,
    target VARCHAR(32) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending_review',
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, feedback_id)
);
CREATE INDEX IF NOT EXISTS idx_feedback_candidates_scope ON feedback_candidates(tenant_id, status, created_at);
