CREATE TRIGGER knowledge_tags_parent_insert_guard
BEFORE INSERT ON knowledge_tags
WHEN NEW.parent_id IS NOT NULL
    AND NOT EXISTS (SELECT 1 FROM knowledge_tags WHERE id = NEW.parent_id)
BEGIN
    SELECT RAISE(ABORT, 'knowledge tag parent does not exist');
END;

CREATE TRIGGER knowledge_tags_parent_update_guard
BEFORE UPDATE OF parent_id ON knowledge_tags
WHEN NEW.parent_id IS NOT NULL
    AND NOT EXISTS (SELECT 1 FROM knowledge_tags WHERE id = NEW.parent_id)
BEGIN
    SELECT RAISE(ABORT, 'knowledge tag parent does not exist');
END;

CREATE TRIGGER knowledge_tags_parent_delete_guard
BEFORE DELETE ON knowledge_tags
WHEN EXISTS (SELECT 1 FROM knowledge_tags WHERE parent_id = OLD.id)
BEGIN
    SELECT RAISE(ABORT, 'knowledge tag parent still has children');
END;
