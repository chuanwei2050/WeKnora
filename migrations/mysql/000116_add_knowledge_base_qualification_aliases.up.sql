ALTER TABLE knowledge_bases
    ADD COLUMN IF NOT EXISTS qualification_aliases JSON NOT NULL DEFAULT (JSON_ARRAY());
