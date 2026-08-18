-- Restore runtime tables that may be absent in databases upgraded from the
-- former Plan3 migration line, whose schema_migrations version already
-- advanced past the original OpenSpec migrations.

CREATE TABLE IF NOT EXISTS acceptance_suite_versions (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    suite_id VARCHAR(36) NOT NULL,
    version_label VARCHAR(64) NOT NULL,
    kind VARCHAR(32) NOT NULL,
    routing_taxonomy_id VARCHAR(128) NOT NULL,
    routing_taxonomy_version VARCHAR(64) NOT NULL,
    frozen BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    frozen_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS acceptance_cases (
    id VARCHAR(36) PRIMARY KEY,
    suite_version_id VARCHAR(36) NOT NULL REFERENCES acceptance_suite_versions(id),
    payload JSONB NOT NULL
);

CREATE TABLE IF NOT EXISTS acceptance_runs (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    suite_version_id VARCHAR(36) NOT NULL REFERENCES acceptance_suite_versions(id),
    profile VARCHAR(32) NOT NULL,
    snapshot JSONB NOT NULL,
    metrics JSONB,
    gate VARCHAR(16) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS acceptance_case_results (
    id VARCHAR(36) PRIMARY KEY,
    run_id VARCHAR(36) NOT NULL REFERENCES acceptance_runs(id),
    case_id VARCHAR(36) NOT NULL,
    payload JSONB NOT NULL
);

CREATE TABLE IF NOT EXISTS acceptance_artifacts (
    id VARCHAR(36) PRIMARY KEY,
    run_id VARCHAR(36) NOT NULL REFERENCES acceptance_runs(id),
    uri TEXT NOT NULL,
    sha256 CHAR(64) NOT NULL,
    size_bytes BIGINT NOT NULL,
    content_type VARCHAR(128),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE acceptance_artifacts ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'report';
CREATE INDEX IF NOT EXISTS idx_acceptance_artifacts_run ON acceptance_artifacts(run_id, created_at);

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
