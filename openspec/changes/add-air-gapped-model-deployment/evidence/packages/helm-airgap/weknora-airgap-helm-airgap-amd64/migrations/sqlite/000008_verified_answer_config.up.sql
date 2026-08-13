-- Migrate the legacy reflection flag into the persisted verification config.
UPDATE custom_agents
SET config = json_set(
    CASE
        WHEN json_type(config, '$.verified_answer') = 'object' THEN config
        ELSE json_set(config, '$.verified_answer', json('{}'))
    END,
    '$.verified_answer.enabled',
    CASE lower(COALESCE(json_extract(config, '$.reflection_enabled'), 'false'))
        WHEN 'true' THEN 1
        ELSE 0
    END
)
WHERE json_type(config, '$.reflection_enabled') IS NOT NULL;
