-- Description: Keep pending governed parsing isolated from the active version.

ALTER TABLE knowledges ADD COLUMN IF NOT EXISTS pending_version_id VARCHAR(36);
CREATE INDEX IF NOT EXISTS idx_knowledges_pending_version ON knowledges(pending_version_id);
