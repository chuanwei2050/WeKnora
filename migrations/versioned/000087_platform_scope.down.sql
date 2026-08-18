-- Migration 000087 down: platform settings are intentionally not copied back
-- to tenant rows because the original ownership is not recoverable after the
-- platform-scope migration.
DROP TABLE IF EXISTS platform_settings;
