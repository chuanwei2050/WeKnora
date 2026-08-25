CREATE TEMP TABLE knowledge_tag_parent_rollback_guard (
    child_count INTEGER NOT NULL CHECK (child_count = 0)
);

INSERT INTO knowledge_tag_parent_rollback_guard (child_count)
SELECT COUNT(*) FROM knowledge_tags WHERE parent_id IS NOT NULL;

DROP TABLE knowledge_tag_parent_rollback_guard;
DROP INDEX IF EXISTS idx_knowledge_tags_parent;
ALTER TABLE knowledge_tags DROP COLUMN parent_id;
