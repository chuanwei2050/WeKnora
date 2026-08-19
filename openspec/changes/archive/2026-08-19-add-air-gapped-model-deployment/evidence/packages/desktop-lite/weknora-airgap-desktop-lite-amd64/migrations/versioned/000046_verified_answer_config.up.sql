-- Migrate the legacy reflection flag into the persisted verification config.
UPDATE custom_agents
SET config = jsonb_set(
    CASE
        WHEN jsonb_typeof(config -> 'verified_answer') = 'object' THEN config
        ELSE jsonb_set(config, '{verified_answer}', '{}'::jsonb, true)
    END,
    '{verified_answer,enabled}',
    to_jsonb(COALESCE((config ->> 'reflection_enabled')::boolean, false)),
    true
)
WHERE config ? 'reflection_enabled';
