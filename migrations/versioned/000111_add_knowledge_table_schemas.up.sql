CREATE TABLE knowledge_table_schemas (
    tenant_id BIGINT NOT NULL,
    knowledge_id VARCHAR(36) NOT NULL,
    revision VARCHAR(128) NOT NULL,
    knowledge_version_id VARCHAR(36) NOT NULL DEFAULT '',
    file_hash VARCHAR(128) NOT NULL DEFAULT '',
    schema_json JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (tenant_id, knowledge_id, revision),
    CONSTRAINT fk_knowledge_table_schemas_knowledge
        FOREIGN KEY (knowledge_id) REFERENCES knowledges(id) ON DELETE CASCADE
);

CREATE INDEX idx_knowledge_table_schemas_version
    ON knowledge_table_schemas (tenant_id, knowledge_version_id);
