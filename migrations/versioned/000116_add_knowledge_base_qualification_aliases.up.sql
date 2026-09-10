ALTER TABLE knowledge_bases
    ADD COLUMN IF NOT EXISTS qualification_aliases JSONB NOT NULL DEFAULT '[]'::jsonb;
