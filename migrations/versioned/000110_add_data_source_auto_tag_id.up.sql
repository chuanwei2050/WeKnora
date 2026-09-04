ALTER TABLE data_sources
ADD COLUMN IF NOT EXISTS auto_tag_id VARCHAR(36);

CREATE INDEX IF NOT EXISTS idx_data_sources_auto_tag_id
ON data_sources(auto_tag_id);
