WITH platform_tenant AS (
    SELECT tenant_id
    FROM users
    WHERE role = 'platform_admin' OR can_access_all_tenants = TRUE
    ORDER BY created_at ASC
    LIMIT 1
)
INSERT INTO tenant_disabled_shared_agents (tenant_id, agent_id, source_tenant_id, created_at)
SELECT platform_tenant.tenant_id, disabled.agent_id, platform_tenant.tenant_id, disabled.created_at
FROM tenant_disabled_shared_agents AS disabled
JOIN platform_tenant ON TRUE
WHERE disabled.tenant_id = 0 AND disabled.source_tenant_id = 0
ON CONFLICT (tenant_id, agent_id, source_tenant_id) DO NOTHING;

DELETE FROM tenant_disabled_shared_agents
WHERE tenant_id = 0 AND source_tenant_id = 0;
