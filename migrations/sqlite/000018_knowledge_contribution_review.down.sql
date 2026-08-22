DROP TABLE IF EXISTS knowledge_contribution_migration_audits;
DROP INDEX IF EXISTS idx_knowledges_created_by;
ALTER TABLE knowledges DROP COLUMN created_by;
ALTER TABLE knowledge_bases DROP COLUMN reviewer_ids;
ALTER TABLE knowledge_bases DROP COLUMN contributor_ids;
ALTER TABLE knowledge_bases DROP COLUMN contribution_mode;
