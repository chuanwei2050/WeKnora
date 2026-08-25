DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM knowledge_tags WHERE parent_id IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot remove knowledge_tags.parent_id while child folders exist';
    END IF;
END $$;

DROP INDEX IF EXISTS idx_knowledge_tags_parent;

ALTER TABLE knowledge_tags
DROP COLUMN IF EXISTS parent_id;
