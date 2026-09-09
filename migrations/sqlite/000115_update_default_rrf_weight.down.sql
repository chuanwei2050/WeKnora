UPDATE platform_settings
SET retrieval_config = json_set(retrieval_config, '$.rrf_vector_weight', 0.7),
    updated_at = CURRENT_TIMESTAMP
WHERE id = 1
  AND retrieval_config IS NOT NULL
  AND CAST(json_extract(retrieval_config, '$.rrf_vector_weight') AS REAL) = 0.5;

UPDATE custom_agents
SET config = json_set(config, '$.rrf_vector_weight', 0.7),
    updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = 0
  AND is_builtin = 1
  AND config IS NOT NULL
  AND CAST(json_extract(config, '$.rrf_vector_weight') AS REAL) = 0.5;
