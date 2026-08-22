-- Restore schema required by the current application after upgrading from the
-- former Plan3 migration line, whose schema_migrations version reached 79.

ALTER TABLE knowledges ADD COLUMN IF NOT EXISTS tag_id VARCHAR(36);
CREATE INDEX IF NOT EXISTS idx_knowledges_tag ON knowledges(tag_id);

CREATE TABLE IF NOT EXISTS organization_members (
    id VARCHAR(36) PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id VARCHAR(36) NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id VARCHAR(36) NOT NULL,
    tenant_id INTEGER NOT NULL,
    role VARCHAR(32) NOT NULL DEFAULT 'viewer',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

DO $$
BEGIN
    IF to_regclass('public.organization_members_pre_plan3') IS NOT NULL THEN
        INSERT INTO organization_members (id, organization_id, user_id, tenant_id, role, created_at, updated_at)
        SELECT id, organization_id, user_id, tenant_id, role, created_at, updated_at
        FROM organization_members_pre_plan3
        ON CONFLICT (id) DO NOTHING;
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS idx_org_members_org_user ON organization_members(organization_id, user_id);
CREATE INDEX IF NOT EXISTS idx_org_members_user_id ON organization_members(user_id);
CREATE INDEX IF NOT EXISTS idx_org_members_tenant_id ON organization_members(tenant_id);
CREATE INDEX IF NOT EXISTS idx_org_members_role ON organization_members(role);
