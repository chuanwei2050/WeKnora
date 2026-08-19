ALTER TABLE platform_settings
ADD COLUMN model_profile TEXT NOT NULL DEFAULT '';

ALTER TABLE models
ADD COLUMN profile TEXT NOT NULL DEFAULT '';

ALTER TABLE models
ADD COLUMN profile_role TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_models_profile ON models(profile);
CREATE INDEX idx_models_profile_role ON models(profile_role);
