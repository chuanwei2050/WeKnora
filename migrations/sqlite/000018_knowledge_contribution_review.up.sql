ALTER TABLE knowledge_bases ADD COLUMN contribution_mode TEXT NOT NULL DEFAULT 'closed' CHECK (contribution_mode IN ('closed', 'members', 'allowlist'));
ALTER TABLE knowledge_bases ADD COLUMN contributor_ids JSON NOT NULL DEFAULT '[]';
ALTER TABLE knowledge_bases ADD COLUMN reviewer_ids JSON NOT NULL DEFAULT '[]';
ALTER TABLE knowledges ADD COLUMN created_by TEXT;
CREATE INDEX idx_knowledges_created_by ON knowledges(created_by);

CREATE TABLE knowledge_contribution_migration_audits (
    knowledge_id TEXT PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    assigned_user_id TEXT NOT NULL,
    migrated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO knowledge_contribution_migration_audits (knowledge_id, tenant_id, assigned_user_id)
SELECT k.id, k.tenant_id,
       COALESCE(
           (SELECT u.id FROM users u WHERE u.tenant_id = k.tenant_id AND u.role = 'tenant_admin' AND u.is_active = 1 ORDER BY u.created_at ASC LIMIT 1),
           (SELECT u.id FROM users u WHERE u.tenant_id = k.tenant_id ORDER BY u.created_at ASC LIMIT 1)
       )
FROM knowledges k
WHERE COALESCE(k.created_by, '') = ''
  AND EXISTS (SELECT 1 FROM users u WHERE u.tenant_id = k.tenant_id);

UPDATE knowledges
SET created_by = (SELECT a.assigned_user_id FROM knowledge_contribution_migration_audits a WHERE a.knowledge_id = knowledges.id)
WHERE COALESCE(created_by, '') = ''
  AND EXISTS (SELECT 1 FROM knowledge_contribution_migration_audits a WHERE a.knowledge_id = knowledges.id);
