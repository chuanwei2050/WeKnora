-- Migration 000087: move model and system configuration to the platform scope.
-- Platform scope uses tenant_id = 0 for shared model records; the tenant table
-- remains tenant data and does not gain a visible "platform tenant" row.

CREATE TABLE IF NOT EXISTS platform_settings (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    retriever_engines JSONB NOT NULL DEFAULT '{"engines": []}'::jsonb,
    agent_config JSONB DEFAULT NULL,
    context_config JSONB DEFAULT NULL,
    web_search_config JSONB DEFAULT NULL,
    conversation_config JSONB DEFAULT NULL,
    parser_engine_config JSONB DEFAULT NULL,
    credentials JSONB DEFAULT NULL,
    storage_engine_config JSONB DEFAULT NULL,
    retrieval_config JSONB DEFAULT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Seed platform settings from the existing platform administrator's legacy
-- tenant row so deployments keep their current configuration after upgrade.
INSERT INTO platform_settings (
    id, retriever_engines, agent_config, context_config, web_search_config,
    conversation_config, parser_engine_config, credentials,
    storage_engine_config, retrieval_config
)
SELECT
    1, COALESCE(t.retriever_engines, '{"engines": []}'::jsonb), t.agent_config, t.context_config, t.web_search_config,
    t.conversation_config, t.parser_engine_config, t.credentials,
    t.storage_engine_config, t.retrieval_config
FROM tenants t
JOIN users u ON u.tenant_id = t.id
WHERE u.role = 'platform_admin' OR u.can_access_all_tenants = TRUE
ORDER BY u.created_at ASC
LIMIT 1
ON CONFLICT (id) DO NOTHING;

INSERT INTO platform_settings (id)
VALUES (1)
ON CONFLICT (id) DO NOTHING;

-- Existing model definitions are configuration, not tenant-owned data. Keep
-- their IDs so knowledge bases remain valid while making them platform-wide.
UPDATE models
SET tenant_id = 0
WHERE tenant_id <> 0 AND COALESCE(is_builtin, FALSE) = FALSE;

-- Search providers, vector stores and MCP services are platform infrastructure
-- as well. Preserve their IDs while making the records visible to every tenant.
UPDATE web_search_providers SET tenant_id = 0 WHERE tenant_id <> 0;

-- Names were unique only inside a tenant. Preserve every live store while
-- making colliding names globally unique before collapsing the scope.
UPDATE vector_stores
SET name = LEFT(name, 210) || ' [' || id || ']'
WHERE tenant_id <> 0
  AND deleted_at IS NULL
  AND name IN (
      SELECT name
      FROM vector_stores
      WHERE deleted_at IS NULL
      GROUP BY name
      HAVING COUNT(*) > 1
  );
UPDATE vector_stores SET tenant_id = 0 WHERE tenant_id <> 0;
UPDATE mcp_services SET tenant_id = 0 WHERE tenant_id <> 0 AND COALESCE(is_builtin, FALSE) = FALSE;

-- Merge tenant-local copies of the same endpoint before the tenant component
-- of the unique key is collapsed to the platform scope.
WITH duplicate_groups AS (
    SELECT scheme, host, port, category, MIN(id) AS canonical_id
    FROM approved_endpoints
    GROUP BY scheme, host, port, category
    HAVING COUNT(*) > 1
), merged_permissions AS (
    SELECT groups.canonical_id,
           COALESCE(jsonb_agg(DISTINCT uses.value) FILTER (WHERE uses.value IS NOT NULL), '[]'::jsonb) AS allowed_uses,
           COALESCE(jsonb_agg(DISTINCT roles.value) FILTER (WHERE roles.value IS NOT NULL), '[]'::jsonb) AS allowed_model_roles
    FROM duplicate_groups AS groups
    JOIN approved_endpoints AS source
      ON source.scheme = groups.scheme
     AND source.host = groups.host
     AND source.port = groups.port
     AND source.category = groups.category
    LEFT JOIN LATERAL jsonb_array_elements(source.allowed_uses) AS uses(value) ON TRUE
    LEFT JOIN LATERAL jsonb_array_elements(source.allowed_model_roles) AS roles(value) ON TRUE
    GROUP BY groups.canonical_id
)
UPDATE approved_endpoints AS canonical
SET allowed_uses = merged.allowed_uses,
    allowed_model_roles = merged.allowed_model_roles
FROM merged_permissions AS merged
WHERE canonical.id = merged.canonical_id;

WITH endpoint_mapping AS (
    SELECT duplicate.id AS duplicate_id, MIN(canonical.id) AS canonical_id
    FROM approved_endpoints AS duplicate
    JOIN approved_endpoints AS canonical
      ON canonical.scheme = duplicate.scheme
     AND canonical.host = duplicate.host
     AND canonical.port = duplicate.port
     AND canonical.category = duplicate.category
    GROUP BY duplicate.id
    HAVING COUNT(*) > 1
)
UPDATE approved_endpoint_audits AS audit
SET endpoint_id = mapping.canonical_id
FROM endpoint_mapping AS mapping
WHERE audit.endpoint_id = mapping.duplicate_id;

DELETE FROM approved_endpoints AS duplicate
WHERE duplicate.id NOT IN (
    SELECT MIN(id)
    FROM approved_endpoints
    GROUP BY scheme, host, port, category
);
UPDATE approved_endpoints SET tenant_id = 0 WHERE tenant_id <> 0;
UPDATE approved_endpoint_audits SET tenant_id = 0 WHERE tenant_id <> 0;
