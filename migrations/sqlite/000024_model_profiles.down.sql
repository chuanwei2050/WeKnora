DROP INDEX IF EXISTS idx_models_profile;
DROP INDEX IF EXISTS idx_models_profile_role;

ALTER TABLE models
DROP COLUMN profile;

ALTER TABLE models
DROP COLUMN profile_role;

ALTER TABLE platform_settings
DROP COLUMN model_profile;
