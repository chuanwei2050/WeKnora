-- Make the legacy source=local shape explicit without changing its API meaning.
-- All fields remain inside models.parameters so older clients can continue to
-- send source/model parameters during the migration window.
UPDATE models
SET parameters = jsonb_set(
    jsonb_set(
        jsonb_set(COALESCE(parameters, '{}'::jsonb), '{protocol}',
            to_jsonb(COALESCE(NULLIF(parameters->>'protocol', ''), 'ollama'::text)), true),
        '{location}',
            to_jsonb(COALESCE(NULLIF(parameters->>'location', ''), 'same-host'::text)), true),
    '{artifact_policy}',
        to_jsonb(COALESCE(NULLIF(parameters->>'artifact_policy', ''), 'preloaded-only'::text)), true)
WHERE source = 'local';

UPDATE models
SET parameters = jsonb_set(COALESCE(parameters, '{}'::jsonb), '{protocol}',
    to_jsonb(COALESCE(NULLIF(parameters->>'protocol', ''), 'openai-compatible'::text)), true)
WHERE source <> 'local';
