UPDATE platform_settings
SET retrieval_config = jsonb_set(retrieval_config, '{rrf_vector_weight}', '0.7'::jsonb, true),
    updated_at = CURRENT_TIMESTAMP
WHERE id = 1
  AND retrieval_config IS NOT NULL
  AND (retrieval_config ->> 'rrf_vector_weight')::double precision = 0.5;

UPDATE custom_agents
SET config = jsonb_set(config, '{rrf_vector_weight}', '0.7'::jsonb, true),
    updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = 0
  AND is_builtin = true
  AND config IS NOT NULL
  AND (config ->> 'rrf_vector_weight')::double precision = 0.5;
