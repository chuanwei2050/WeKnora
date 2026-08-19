ALTER TABLE platform_settings
ADD COLUMN IF NOT EXISTS model_profile VARCHAR(16) NOT NULL DEFAULT '';

ALTER TABLE models
ADD COLUMN IF NOT EXISTS profile VARCHAR(16) NOT NULL DEFAULT '';

ALTER TABLE models
ADD COLUMN IF NOT EXISTS profile_role VARCHAR(32) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_models_profile ON models(profile);
CREATE INDEX IF NOT EXISTS idx_models_profile_role ON models(profile_role);
