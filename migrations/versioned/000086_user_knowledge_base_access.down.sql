ALTER TABLE users DROP CONSTRAINT IF EXISTS chk_users_knowledge_base_access_mode;
ALTER TABLE users DROP COLUMN IF EXISTS knowledge_base_ids;
ALTER TABLE users DROP COLUMN IF EXISTS knowledge_base_access_mode;
