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
