ALTER TABLE knowledge_bases
    ADD COLUMN qualification_aliases JSON NOT NULL DEFAULT (JSON_ARRAY());
