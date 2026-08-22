ALTER TABLE users ADD COLUMN knowledge_base_access_mode TEXT NOT NULL DEFAULT 'all';
ALTER TABLE users ADD COLUMN knowledge_base_ids TEXT NOT NULL DEFAULT '[]';
