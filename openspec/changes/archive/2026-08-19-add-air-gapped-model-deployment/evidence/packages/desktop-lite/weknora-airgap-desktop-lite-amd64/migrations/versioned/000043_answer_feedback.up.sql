CREATE TABLE IF NOT EXISTS answer_feedback (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    session_id VARCHAR(36) NOT NULL,
    message_id VARCHAR(36) NOT NULL,
    answer_version VARCHAR(128) NOT NULL,
    rating INTEGER NOT NULL,
    correction TEXT,
    target VARCHAR(32),
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    reviewer_id VARCHAR(36),
    candidate_id VARCHAR(36),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (message_id, answer_version)
);
