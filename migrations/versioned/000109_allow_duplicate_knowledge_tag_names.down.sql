DROP INDEX IF EXISTS idx_knowledge_tags_untagged;

CREATE UNIQUE INDEX IF NOT EXISTS idx_knowledge_tags_kb_name
ON knowledge_tags(tenant_id, knowledge_base_id, name);
