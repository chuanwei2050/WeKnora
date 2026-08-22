UPDATE models
SET name = 'Qwen3.5-122B-A10B-FP8',
    parameters = jsonb_set(parameters, '{api_key}', to_jsonb('0d83ab7d261c0a74acb5d7ed417706088b55f26c8d61d2eaed9c462de4366232'::text)),
    updated_at = NOW()
WHERE tenant_id = 0
  AND profile = 'offline'
  AND profile_role IN ('chat', 'verifier_2', 'evaluation_judge', 'vlm');

UPDATE models
SET name = 'deepseek-ai/DeepSeek-V4-Pro',
    updated_at = NOW()
WHERE tenant_id = 0
  AND profile = 'online'
  AND profile_role IN ('chat', 'evaluation_judge', 'vlm');

SELECT name, profile, profile_role, parameters->>'base_url' AS base_url
FROM models
WHERE tenant_id = 0
  AND profile_role IN ('chat', 'evaluation_judge', 'verifier_2', 'vlm')
ORDER BY profile, profile_role;
