ALTER TABLE users
    ADD COLUMN IF NOT EXISTS knowledge_base_access_mode VARCHAR(16) NOT NULL DEFAULT 'all';

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS knowledge_base_ids JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE users
    ADD CONSTRAINT chk_users_knowledge_base_access_mode
    CHECK (knowledge_base_access_mode IN ('all', 'selected'));
