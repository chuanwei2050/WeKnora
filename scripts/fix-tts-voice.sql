UPDATE models
SET parameters = jsonb_set(
  parameters,
  '{extra_config}',
  '{"voice": "FunAudioLLM/CosyVoice2-0.5B:alex", "format": "mp3"}'::jsonb
),
updated_at = NOW()
WHERE tenant_id = 0 AND profile = 'online' AND profile_role = 'tts';

UPDATE models
SET parameters = jsonb_set(
  parameters,
  '{extra_config}',
  '{"voice": "alex", "format": "mp3"}'::jsonb
),
updated_at = NOW()
WHERE tenant_id = 0 AND profile = 'offline' AND profile_role = 'tts';

SELECT profile, profile_role, name, parameters->'extra_config' AS extra
FROM models
WHERE type = 'TTS' AND tenant_id = 0;
