-- Promote the platform administrator's existing platform-agent preferences to global status.
WITH platform_tenant AS (
    SELECT tenant_id
    FROM users
    WHERE role = 'platform_admin' OR can_access_all_tenants = TRUE
    ORDER BY created_at ASC
    LIMIT 1
)
INSERT INTO tenant_disabled_shared_agents (tenant_id, agent_id, source_tenant_id, created_at)
SELECT 0, disabled.agent_id, 0, MIN(disabled.created_at)
FROM tenant_disabled_shared_agents AS disabled
JOIN platform_tenant ON platform_tenant.tenant_id = disabled.tenant_id
WHERE disabled.source_tenant_id IN (0, disabled.tenant_id)
  AND EXISTS (
      SELECT 1 FROM custom_agents AS platform_agent
      WHERE platform_agent.tenant_id = 0 AND platform_agent.id = disabled.agent_id
  )
GROUP BY disabled.agent_id
ON CONFLICT (tenant_id, agent_id, source_tenant_id) DO NOTHING;

-- Platform-agent status is global now; discard obsolete tenant-specific copies.
DELETE FROM tenant_disabled_shared_agents
WHERE tenant_id <> 0
  AND (
      source_tenant_id = 0
      OR (
          source_tenant_id = (
              SELECT tenant_id
              FROM users
              WHERE role = 'platform_admin' OR can_access_all_tenants = TRUE
              ORDER BY created_at ASC
              LIMIT 1
          )
          AND EXISTS (
              SELECT 1 FROM custom_agents AS platform_agent
              WHERE platform_agent.tenant_id = 0
                AND platform_agent.id = tenant_disabled_shared_agents.agent_id
          )
      )
  );
