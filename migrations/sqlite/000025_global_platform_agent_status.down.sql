INSERT OR IGNORE INTO tenant_disabled_shared_agents (tenant_id, agent_id, source_tenant_id, created_at)
SELECT platform_tenant.tenant_id, disabled.agent_id, platform_tenant.tenant_id, disabled.created_at
FROM tenant_disabled_shared_agents AS disabled
JOIN (
    SELECT tenant_id
    FROM users
    WHERE role = 'platform_admin' OR COALESCE(can_access_all_tenants, 0) = 1
    ORDER BY created_at ASC
    LIMIT 1
) AS platform_tenant
WHERE disabled.tenant_id = 0 AND disabled.source_tenant_id = 0;

DELETE FROM tenant_disabled_shared_agents
WHERE tenant_id = 0 AND source_tenant_id = 0;
