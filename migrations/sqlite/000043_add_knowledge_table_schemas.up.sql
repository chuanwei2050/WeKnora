CREATE TABLE knowledge_table_schemas (
    tenant_id INTEGER NOT NULL,
    knowledge_id TEXT NOT NULL,
    revision TEXT NOT NULL,
    knowledge_version_id TEXT NOT NULL DEFAULT '',
    file_hash TEXT NOT NULL DEFAULT '',
    schema_json TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (tenant_id, knowledge_id, revision),
    FOREIGN KEY (knowledge_id) REFERENCES knowledges(id) ON DELETE CASCADE
);

CREATE INDEX idx_knowledge_table_schemas_version
    ON knowledge_table_schemas (tenant_id, knowledge_version_id);
