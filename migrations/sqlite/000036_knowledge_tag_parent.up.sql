ALTER TABLE knowledge_tags ADD COLUMN parent_id TEXT;

CREATE INDEX IF NOT EXISTS idx_knowledge_tags_parent
ON knowledge_tags (tenant_id, knowledge_base_id, parent_id);
