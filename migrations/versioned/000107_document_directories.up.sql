CREATE TABLE knowledge_directories (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    parent_id VARCHAR(36),
    parent_key VARCHAR(36) NOT NULL DEFAULT '',
    name VARCHAR(255) NOT NULL,
    normalized_name VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    deletion_task_id VARCHAR(36),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_knowledge_directories_scope_id UNIQUE (tenant_id, knowledge_base_id, id),
    CONSTRAINT fk_knowledge_directories_parent FOREIGN KEY (tenant_id, knowledge_base_id, parent_id) REFERENCES knowledge_directories(tenant_id, knowledge_base_id, id) ON DELETE RESTRICT,
    CONSTRAINT fk_knowledge_directories_kb FOREIGN KEY (knowledge_base_id) REFERENCES knowledge_bases(id) ON DELETE CASCADE,
    CONSTRAINT uq_knowledge_directories_sibling UNIQUE (tenant_id, knowledge_base_id, parent_key, normalized_name)
);

CREATE INDEX idx_knowledge_directories_parent ON knowledge_directories(tenant_id, knowledge_base_id, parent_key, status);
ALTER TABLE knowledges ADD COLUMN directory_id VARCHAR(36);
ALTER TABLE knowledges ADD COLUMN deletion_task_id VARCHAR(36);
ALTER TABLE knowledges ADD CONSTRAINT fk_knowledges_directory FOREIGN KEY (tenant_id, knowledge_base_id, directory_id) REFERENCES knowledge_directories(tenant_id, knowledge_base_id, id) ON DELETE RESTRICT;
CREATE INDEX idx_knowledges_directory ON knowledges(tenant_id, knowledge_base_id, directory_id);
CREATE INDEX idx_knowledges_deletion_task ON knowledges(tenant_id, deletion_task_id);

CREATE TABLE knowledge_directory_delete_tasks (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    root_directory_id VARCHAR(36) NOT NULL,
    requested_by VARCHAR(36) NOT NULL,
    snapshot_digest VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    failure_summary TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE knowledge_directory_delete_batches (
    id VARCHAR(36) PRIMARY KEY,
    delete_task_id VARCHAR(36) NOT NULL REFERENCES knowledge_directory_delete_tasks(id) ON DELETE CASCADE,
    asynq_task_id VARCHAR(64) NOT NULL UNIQUE,
    knowledge_ids JSONB NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    failure_summary TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_directory_delete_batches_pending ON knowledge_directory_delete_batches(status, created_at);

CREATE TABLE knowledge_directory_delete_tokens (
    token_hash VARCHAR(64) PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    root_directory_id VARCHAR(36) NOT NULL,
    requested_by VARCHAR(36) NOT NULL,
    snapshot_digest VARCHAR(64) NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    consumed_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);
