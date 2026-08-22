-- Promote the platform administrator's existing agents to the shared platform scope.
-- Tenant-owned rows are retained for rollback/audit, but are no longer effective.
WITH platform_tenant AS (
    SELECT tenant_id
    FROM users
    WHERE role = 'platform_admin' OR can_access_all_tenants = TRUE
    ORDER BY created_at ASC
    LIMIT 1
)
INSERT INTO custom_agents (
    id, name, description, avatar, is_builtin, tenant_id, created_by,
    config, created_at, updated_at, deleted_at
)
SELECT
    agent.id, agent.name, agent.description, agent.avatar, agent.is_builtin, 0,
    agent.created_by, agent.config, agent.created_at, agent.updated_at, agent.deleted_at
FROM custom_agents AS agent
JOIN platform_tenant ON platform_tenant.tenant_id = agent.tenant_id
WHERE agent.deleted_at IS NULL
ON CONFLICT (id, tenant_id) DO NOTHING;
