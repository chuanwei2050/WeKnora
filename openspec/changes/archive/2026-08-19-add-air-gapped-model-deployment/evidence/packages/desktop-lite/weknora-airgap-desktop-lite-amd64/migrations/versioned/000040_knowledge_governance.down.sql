DROP TABLE IF EXISTS knowledge_version_reviews;
DROP TABLE IF EXISTS knowledge_versions;
DROP INDEX IF EXISTS idx_chunks_knowledge_version;
DROP INDEX IF EXISTS idx_knowledges_current_version;
ALTER TABLE chunks DROP COLUMN IF EXISTS knowledge_version_id;
ALTER TABLE knowledges DROP COLUMN IF EXISTS current_version_id;
ALTER TABLE knowledge_bases DROP COLUMN IF EXISTS governance;
