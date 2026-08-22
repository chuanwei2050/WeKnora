DROP INDEX IF EXISTS idx_knowledge_bases_tenant_created_by;

ALTER TABLE knowledge_bases DROP COLUMN created_by;

ALTER TABLE users DROP COLUMN bidreview_role;
