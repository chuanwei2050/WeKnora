ALTER TABLE users ADD COLUMN bidreview_role VARCHAR(32) NOT NULL DEFAULT 'member';

ALTER TABLE knowledge_bases ADD COLUMN created_by VARCHAR(36);

CREATE INDEX IF NOT EXISTS idx_knowledge_bases_tenant_created_by
    ON knowledge_bases(tenant_id, created_by);
