DROP TABLE IF EXISTS knowledge_version_reviews;
DROP TABLE IF EXISTS knowledge_versions;
DROP INDEX IF EXISTS idx_chunks_knowledge_version;
ALTER TABLE chunks DROP COLUMN knowledge_version_id;
ALTER TABLE knowledges DROP COLUMN current_version_id;
ALTER TABLE knowledge_bases DROP COLUMN governance;
