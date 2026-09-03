INSERT INTO knowledge_tags (id, tenant_id, knowledge_base_id, name, color, sort_order, created_at, updated_at)
SELECT gen_random_uuid()::VARCHAR(36), kb.tenant_id, kb.id, '未分类', '', -1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM knowledge_bases kb
WHERE NOT EXISTS (
    SELECT 1 FROM knowledge_tags tag
    WHERE tag.tenant_id = kb.tenant_id AND tag.knowledge_base_id = kb.id AND tag.name = '未分类'
);

ALTER TABLE knowledge_directories ADD COLUMN tag_id VARCHAR(36);
UPDATE knowledge_directories directory
SET tag_id = tag.id
FROM knowledge_tags tag
WHERE tag.tenant_id = directory.tenant_id
  AND tag.knowledge_base_id = directory.knowledge_base_id
  AND tag.name = '未分类';
UPDATE knowledges knowledge
SET tag_id = directory.tag_id
FROM knowledge_directories directory
WHERE knowledge.tenant_id = directory.tenant_id
  AND knowledge.knowledge_base_id = directory.knowledge_base_id
  AND knowledge.directory_id = directory.id;
ALTER TABLE knowledge_directories ALTER COLUMN tag_id SET NOT NULL;

ALTER TABLE knowledges DROP CONSTRAINT IF EXISTS fk_knowledges_directory;
ALTER TABLE knowledge_directories DROP CONSTRAINT IF EXISTS fk_knowledge_directories_parent;
ALTER TABLE knowledge_directories DROP CONSTRAINT IF EXISTS uq_knowledge_directories_scope_id;
ALTER TABLE knowledge_directories DROP CONSTRAINT IF EXISTS uq_knowledge_directories_sibling;
DROP INDEX IF EXISTS idx_knowledge_directories_parent;
DROP INDEX IF EXISTS idx_knowledges_directory;

CREATE UNIQUE INDEX IF NOT EXISTS uq_knowledge_tags_scope_id ON knowledge_tags(tenant_id, knowledge_base_id, id);
ALTER TABLE knowledge_directories ADD CONSTRAINT uq_knowledge_directories_scope_id UNIQUE (tenant_id, knowledge_base_id, tag_id, id);
ALTER TABLE knowledge_directories ADD CONSTRAINT uq_knowledge_directories_sibling UNIQUE (tenant_id, knowledge_base_id, tag_id, parent_key, normalized_name);
ALTER TABLE knowledge_directories ADD CONSTRAINT fk_knowledge_directories_tag FOREIGN KEY (tenant_id, knowledge_base_id, tag_id) REFERENCES knowledge_tags(tenant_id, knowledge_base_id, id) ON DELETE RESTRICT;
ALTER TABLE knowledge_directories ADD CONSTRAINT fk_knowledge_directories_parent FOREIGN KEY (tenant_id, knowledge_base_id, tag_id, parent_id) REFERENCES knowledge_directories(tenant_id, knowledge_base_id, tag_id, id) ON DELETE RESTRICT;
ALTER TABLE knowledges ADD CONSTRAINT fk_knowledges_directory FOREIGN KEY (tenant_id, knowledge_base_id, tag_id, directory_id) REFERENCES knowledge_directories(tenant_id, knowledge_base_id, tag_id, id) ON DELETE RESTRICT;
CREATE INDEX idx_knowledge_directories_parent ON knowledge_directories(tenant_id, knowledge_base_id, tag_id, parent_key, status);
CREATE INDEX idx_knowledges_directory ON knowledges(tenant_id, knowledge_base_id, tag_id, directory_id);
