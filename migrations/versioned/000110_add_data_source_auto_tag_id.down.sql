DROP INDEX IF EXISTS idx_data_sources_auto_tag_id;

ALTER TABLE data_sources
DROP COLUMN IF EXISTS auto_tag_id;
