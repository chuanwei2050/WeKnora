DROP TABLE IF EXISTS knowledge_directory_delete_tokens;
DROP TABLE IF EXISTS knowledge_directory_delete_batches;
DROP TABLE IF EXISTS knowledge_directory_delete_tasks;
DROP INDEX IF EXISTS idx_knowledges_deletion_task;
DROP INDEX IF EXISTS idx_knowledges_directory;
ALTER TABLE knowledges DROP CONSTRAINT IF EXISTS fk_knowledges_directory;
ALTER TABLE knowledges DROP COLUMN IF EXISTS deletion_task_id;
ALTER TABLE knowledges DROP COLUMN IF EXISTS directory_id;
DROP TABLE IF EXISTS knowledge_directories;
