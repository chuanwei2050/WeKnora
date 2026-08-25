DROP INDEX idx_knowledge_bases_tenant_code_key;
ALTER TABLE knowledge_bases DROP COLUMN code_key;
ALTER TABLE knowledge_bases DROP COLUMN code;
