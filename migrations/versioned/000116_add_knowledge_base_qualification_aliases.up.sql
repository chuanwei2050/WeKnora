ALTER TABLE knowledge_bases
    ADD COLUMN qualification_aliases JSONB NOT NULL DEFAULT '[]'::jsonb;
