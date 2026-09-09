UPDATE platform_settings
SET retrieval_config = JSON_SET(retrieval_config, '$.rrf_vector_weight', 0.7),
    updated_at = CURRENT_TIMESTAMP
WHERE id = 1
  AND retrieval_config IS NOT NULL
  AND CAST(JSON_UNQUOTE(JSON_EXTRACT(retrieval_config, '$.rrf_vector_weight')) AS DECIMAL(3,2)) = 0.50;

UPDATE custom_agents
SET config = JSON_SET(config, '$.rrf_vector_weight', 0.7),
    updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = 0
  AND is_builtin = TRUE
  AND config IS NOT NULL
  AND CAST(JSON_UNQUOTE(JSON_EXTRACT(config, '$.rrf_vector_weight')) AS DECIMAL(3,2)) = 0.50;
