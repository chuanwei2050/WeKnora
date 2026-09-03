CREATE TEMP TABLE directory_scope_rollback_guard(value INTEGER CHECK(value = 0));
INSERT INTO directory_scope_rollback_guard(value)
SELECT COUNT(*) FROM (SELECT 1 FROM knowledge_directories GROUP BY tenant_id, knowledge_base_id, parent_key, normalized_name HAVING COUNT(*) > 1);
DROP TABLE directory_scope_rollback_guard;
CREATE TEMP TABLE document_directory_assignments AS
SELECT id AS knowledge_id, directory_id FROM knowledges WHERE directory_id IS NOT NULL;
UPDATE knowledges SET directory_id = NULL WHERE directory_id IS NOT NULL;

CREATE TABLE knowledge_directories_unscoped (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    parent_id VARCHAR(36),
    parent_key VARCHAR(36) NOT NULL DEFAULT '',
    name VARCHAR(255) NOT NULL,
    normalized_name VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    deletion_task_id VARCHAR(36),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (tenant_id, knowledge_base_id, id),
    FOREIGN KEY (tenant_id, knowledge_base_id, parent_id) REFERENCES knowledge_directories_unscoped(tenant_id, knowledge_base_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (knowledge_base_id) REFERENCES knowledge_bases(id) ON DELETE CASCADE,
    UNIQUE (tenant_id, knowledge_base_id, parent_key, normalized_name)
);
INSERT INTO knowledge_directories_unscoped (id, tenant_id, knowledge_base_id, parent_id, parent_key, name, normalized_name, status, deletion_task_id, created_at, updated_at)
SELECT id, tenant_id, knowledge_base_id, parent_id, parent_key, name, normalized_name, status, deletion_task_id, created_at, updated_at
FROM knowledge_directories;
DROP TABLE knowledge_directories;
ALTER TABLE knowledge_directories_unscoped RENAME TO knowledge_directories;
CREATE INDEX idx_knowledge_directories_parent ON knowledge_directories(tenant_id, knowledge_base_id, parent_key, status);
UPDATE knowledges
SET directory_id = (SELECT assignment.directory_id FROM document_directory_assignments assignment WHERE assignment.knowledge_id = knowledges.id)
WHERE id IN (SELECT knowledge_id FROM document_directory_assignments);
DROP TABLE document_directory_assignments;
DROP INDEX IF EXISTS idx_knowledges_directory;
CREATE INDEX idx_knowledges_directory ON knowledges(tenant_id, knowledge_base_id, directory_id);
