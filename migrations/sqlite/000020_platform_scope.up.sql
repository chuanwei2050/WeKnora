-- Migration 000020: move platform configuration to a shared scope.

CREATE TABLE IF NOT EXISTS platform_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    retriever_engines TEXT NOT NULL DEFAULT '{"engines": []}',
    agent_config TEXT,
    context_config TEXT,
    web_search_config TEXT,
    conversation_config TEXT,
    parser_engine_config TEXT,
    credentials TEXT,
    storage_engine_config TEXT,
    retrieval_config TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO platform_settings (
    id, retriever_engines, agent_config, context_config, web_search_config,
    conversation_config, parser_engine_config, credentials,
    storage_engine_config, retrieval_config
)
SELECT
    1, COALESCE(t.retriever_engines, '{"engines": []}'), t.agent_config, t.context_config, t.web_search_config,
    t.conversation_config, t.parser_engine_config, t.credentials,
    t.storage_engine_config, t.retrieval_config
FROM tenants t
JOIN users u ON u.tenant_id = t.id
WHERE u.role = 'platform_admin' OR COALESCE(u.can_access_all_tenants, 0) = 1
ORDER BY u.created_at ASC
LIMIT 1;

INSERT OR IGNORE INTO platform_settings (id) VALUES (1);

UPDATE models
SET tenant_id = 0
WHERE tenant_id <> 0 AND COALESCE(is_builtin, 0) = 0;

UPDATE web_search_providers SET tenant_id = 0 WHERE tenant_id <> 0;

-- Names were unique only inside a tenant. Preserve every live store while
-- making colliding names globally unique before collapsing the scope.
UPDATE vector_stores
SET name = substr(name, 1, 210) || ' [' || id || ']'
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
UPDATE mcp_services SET tenant_id = 0 WHERE tenant_id <> 0 AND COALESCE(is_builtin, 0) = 0;

-- The same approved endpoint may have been registered independently by
-- multiple tenants. Keep one canonical row, merge its permissions, and point
-- historical audits at it before applying the global unique scope.
UPDATE approved_endpoints AS canonical
SET allowed_uses = COALESCE((
        SELECT json_group_array(DISTINCT uses.value)
        FROM approved_endpoints AS source, json_each(source.allowed_uses) AS uses
        WHERE source.scheme = canonical.scheme
          AND source.host = canonical.host
          AND source.port = canonical.port
          AND source.category = canonical.category
    ), '[]'),
    allowed_model_roles = COALESCE((
        SELECT json_group_array(DISTINCT roles.value)
        FROM approved_endpoints AS source, json_each(source.allowed_model_roles) AS roles
        WHERE source.scheme = canonical.scheme
          AND source.host = canonical.host
          AND source.port = canonical.port
          AND source.category = canonical.category
    ), '[]')
WHERE canonical.id IN (
    SELECT MIN(id)
    FROM approved_endpoints
    GROUP BY scheme, host, port, category
    HAVING COUNT(*) > 1
);

UPDATE approved_endpoint_audits
SET endpoint_id = (
    SELECT MIN(canonical.id)
    FROM approved_endpoints AS duplicate
    JOIN approved_endpoints AS canonical
      ON canonical.scheme = duplicate.scheme
     AND canonical.host = duplicate.host
     AND canonical.port = duplicate.port
     AND canonical.category = duplicate.category
    WHERE duplicate.id = approved_endpoint_audits.endpoint_id
)
WHERE endpoint_id IN (
    SELECT duplicate.id
    FROM approved_endpoints AS duplicate
    JOIN approved_endpoints AS canonical
      ON canonical.scheme = duplicate.scheme
     AND canonical.host = duplicate.host
     AND canonical.port = duplicate.port
     AND canonical.category = duplicate.category
    GROUP BY duplicate.id
    HAVING COUNT(*) > 1
);

DELETE FROM approved_endpoints
WHERE id NOT IN (
    SELECT MIN(id)
    FROM approved_endpoints
    GROUP BY scheme, host, port, category
);
UPDATE approved_endpoints SET tenant_id = 0 WHERE tenant_id <> 0;
UPDATE approved_endpoint_audits SET tenant_id = 0 WHERE tenant_id <> 0;
