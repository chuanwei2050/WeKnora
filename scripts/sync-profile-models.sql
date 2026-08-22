-- Sync platform profile model names after env/catalog updates.
-- API keys remain in parameters from bootstrap; this script only renames models.

UPDATE models
SET name = 'Qwen3.5-122B-A10B-FP8',
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
