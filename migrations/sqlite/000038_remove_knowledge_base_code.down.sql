ALTER TABLE knowledge_bases ADD COLUMN code VARCHAR(64);
ALTER TABLE knowledge_bases ADD COLUMN code_key VARCHAR(64);
CREATE UNIQUE INDEX idx_knowledge_bases_tenant_code_key ON knowledge_bases (tenant_id, code_key);
