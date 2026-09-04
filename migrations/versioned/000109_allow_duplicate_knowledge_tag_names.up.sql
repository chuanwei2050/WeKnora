DROP INDEX IF EXISTS idx_knowledge_tags_kb_name;

CREATE UNIQUE INDEX IF NOT EXISTS idx_knowledge_tags_untagged
ON knowledge_tags(tenant_id, knowledge_base_id)
WHERE name = '未分类';
