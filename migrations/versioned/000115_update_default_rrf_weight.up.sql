-- Keep existing installations aligned with the platform RRF default changed
-- from 0.7/0.3 to 0.5/0.5. Only the legacy default is migrated; any other
-- explicitly configured value is preserved.
UPDATE platform_settings
SET retrieval_config = jsonb_set(retrieval_config, '{rrf_vector_weight}', '0.5'::jsonb, true),
    updated_at = CURRENT_TIMESTAMP
WHERE id = 1
  AND retrieval_config IS NOT NULL
  AND (retrieval_config ->> 'rrf_vector_weight')::double precision = 0.7;

-- Built-in agent records still expose the historical value in their read
-- model. They do not own platform retrieval settings, but synchronizing the
-- legacy default avoids presenting a value that is not used by retrieval.
UPDATE custom_agents
SET config = jsonb_set(config, '{rrf_vector_weight}', '0.5'::jsonb, true),
    updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = 0
  AND is_builtin = true
  AND config IS NOT NULL
  AND (config ->> 'rrf_vector_weight')::double precision = 0.7;
