ALTER TABLE knowledges ADD COLUMN pending_version_id TEXT;
CREATE INDEX idx_knowledges_pending_version ON knowledges(pending_version_id);
