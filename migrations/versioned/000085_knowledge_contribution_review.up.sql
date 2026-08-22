ALTER TABLE knowledge_bases
    ADD COLUMN IF NOT EXISTS contribution_mode VARCHAR(20) NOT NULL DEFAULT 'closed';
ALTER TABLE knowledge_bases
    ADD COLUMN IF NOT EXISTS contributor_ids JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE knowledge_bases
    ADD COLUMN IF NOT EXISTS reviewer_ids JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE knowledge_bases
    ADD CONSTRAINT chk_knowledge_bases_contribution_mode
    CHECK (contribution_mode IN ('closed', 'members', 'allowlist'));

ALTER TABLE knowledges ADD COLUMN IF NOT EXISTS created_by VARCHAR(36);
CREATE INDEX IF NOT EXISTS idx_knowledges_created_by ON knowledges(created_by);

CREATE TABLE IF NOT EXISTS knowledge_contribution_migration_audits (
    knowledge_id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    assigned_user_id VARCHAR(36) NOT NULL,
    migrated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

WITH assignments AS (
    SELECT k.id AS knowledge_id, k.tenant_id,
           COALESCE(
               (SELECT u.id FROM users u WHERE u.tenant_id = k.tenant_id AND u.role = 'tenant_admin' AND u.is_active = TRUE ORDER BY u.created_at ASC LIMIT 1),
               (SELECT u.id FROM users u WHERE u.tenant_id = k.tenant_id ORDER BY u.created_at ASC LIMIT 1)
           ) AS assigned_user_id
    FROM knowledges k
    WHERE COALESCE(k.created_by, '') = ''
)
INSERT INTO knowledge_contribution_migration_audits (knowledge_id, tenant_id, assigned_user_id)
SELECT knowledge_id, tenant_id, assigned_user_id FROM assignments WHERE assigned_user_id IS NOT NULL
ON CONFLICT (knowledge_id) DO NOTHING;

UPDATE knowledges k
SET created_by = a.assigned_user_id
FROM knowledge_contribution_migration_audits a
WHERE a.knowledge_id = k.id AND COALESCE(k.created_by, '') = '';
