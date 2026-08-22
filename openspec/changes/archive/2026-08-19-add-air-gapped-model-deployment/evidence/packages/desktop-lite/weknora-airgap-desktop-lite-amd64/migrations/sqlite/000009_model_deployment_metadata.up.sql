-- Make the legacy source=local shape explicit while keeping the old API.
UPDATE models
SET parameters = json_set(
    json_set(
        json_set(COALESCE(parameters, '{}'), '$.protocol',
            COALESCE(NULLIF(json_extract(parameters, '$.protocol'), ''), 'ollama')),
        '$.location',
            COALESCE(NULLIF(json_extract(parameters, '$.location'), ''), 'same-host')),
    '$.artifact_policy',
        COALESCE(NULLIF(json_extract(parameters, '$.artifact_policy'), ''), 'preloaded-only'))
WHERE source = 'local';

UPDATE models
SET parameters = json_set(COALESCE(parameters, '{}'), '$.protocol',
    COALESCE(NULLIF(json_extract(parameters, '$.protocol'), ''), 'openai-compatible'))
WHERE source <> 'local';
