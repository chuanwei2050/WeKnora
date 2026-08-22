DROP INDEX IF EXISTS idx_models_profile;
DROP INDEX IF EXISTS idx_models_profile_role;

ALTER TABLE models
DROP COLUMN IF EXISTS profile;

ALTER TABLE models
DROP COLUMN IF EXISTS profile_role;

ALTER TABLE platform_settings
DROP COLUMN IF EXISTS model_profile;
