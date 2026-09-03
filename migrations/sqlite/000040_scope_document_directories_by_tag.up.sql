WITH missing_untagged AS (
    SELECT kb.id AS knowledge_base_id, kb.tenant_id
    FROM knowledge_bases kb
    WHERE NOT EXISTS (SELECT 1 FROM knowledge_tags tag WHERE tag.tenant_id = kb.tenant_id AND tag.knowledge_base_id = kb.id AND tag.name = '未分类')
), sequence_base AS (SELECT COALESCE(MAX(seq_id), 0) AS max_seq_id FROM knowledge_tags)
INSERT INTO knowledge_tags (id, tenant_id, knowledge_base_id, name, color, sort_order, seq_id, created_at, updated_at)
SELECT lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-' || hex(randomblob(2)) || '-' || hex(randomblob(2)) || '-' || hex(randomblob(6))),
       missing.tenant_id, missing.knowledge_base_id, '未分类', '', -1,
       sequence_base.max_seq_id + ROW_NUMBER() OVER (ORDER BY missing.knowledge_base_id), CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM missing_untagged missing CROSS JOIN sequence_base;

CREATE TEMP TABLE document_directory_assignments AS
SELECT id AS knowledge_id, directory_id FROM knowledges WHERE directory_id IS NOT NULL;
UPDATE knowledges SET directory_id = NULL WHERE directory_id IS NOT NULL;

CREATE TABLE knowledge_directories_scoped (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    tag_id VARCHAR(36) NOT NULL,
    parent_id VARCHAR(36),
    parent_key VARCHAR(36) NOT NULL DEFAULT '',
    name VARCHAR(255) NOT NULL,
    normalized_name VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    deletion_task_id VARCHAR(36),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (tenant_id, knowledge_base_id, tag_id, id),
    FOREIGN KEY (tenant_id, knowledge_base_id, tag_id, parent_id) REFERENCES knowledge_directories_scoped(tenant_id, knowledge_base_id, tag_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (tag_id) REFERENCES knowledge_tags(id) ON DELETE RESTRICT,
    FOREIGN KEY (knowledge_base_id) REFERENCES knowledge_bases(id) ON DELETE CASCADE,
    UNIQUE (tenant_id, knowledge_base_id, tag_id, parent_key, normalized_name)
);
INSERT INTO knowledge_directories_scoped (id, tenant_id, knowledge_base_id, tag_id, parent_id, parent_key, name, normalized_name, status, deletion_task_id, created_at, updated_at)
SELECT directory.id, directory.tenant_id, directory.knowledge_base_id, tag.id, directory.parent_id, directory.parent_key, directory.name, directory.normalized_name, directory.status, directory.deletion_task_id, directory.created_at, directory.updated_at
FROM knowledge_directories directory
JOIN knowledge_tags tag ON tag.tenant_id = directory.tenant_id AND tag.knowledge_base_id = directory.knowledge_base_id AND tag.name = '未分类';
DROP TABLE knowledge_directories;
ALTER TABLE knowledge_directories_scoped RENAME TO knowledge_directories;
CREATE INDEX idx_knowledge_directories_parent ON knowledge_directories(tenant_id, knowledge_base_id, tag_id, parent_key, status);

UPDATE knowledges
SET tag_id = (SELECT directory.tag_id FROM knowledge_directories directory JOIN document_directory_assignments assignment ON assignment.directory_id = directory.id WHERE assignment.knowledge_id = knowledges.id),
    directory_id = (SELECT assignment.directory_id FROM document_directory_assignments assignment WHERE assignment.knowledge_id = knowledges.id)
WHERE id IN (SELECT knowledge_id FROM document_directory_assignments);
DROP TABLE document_directory_assignments;
DROP INDEX IF EXISTS idx_knowledges_directory;
CREATE INDEX idx_knowledges_directory ON knowledges(tenant_id, knowledge_base_id, tag_id, directory_id);
