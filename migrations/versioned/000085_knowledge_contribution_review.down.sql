DROP TABLE IF EXISTS knowledge_contribution_migration_audits;
DROP INDEX IF EXISTS idx_knowledges_created_by;
ALTER TABLE knowledges DROP COLUMN IF EXISTS created_by;
ALTER TABLE knowledge_bases DROP CONSTRAINT IF EXISTS chk_knowledge_bases_contribution_mode;
ALTER TABLE knowledge_bases DROP COLUMN IF EXISTS reviewer_ids;
ALTER TABLE knowledge_bases DROP COLUMN IF EXISTS contributor_ids;
ALTER TABLE knowledge_bases DROP COLUMN IF EXISTS contribution_mode;
