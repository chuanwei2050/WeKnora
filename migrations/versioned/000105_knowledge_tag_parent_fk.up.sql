ALTER TABLE knowledge_tags
ADD CONSTRAINT fk_knowledge_tags_parent
FOREIGN KEY (parent_id) REFERENCES knowledge_tags(id) ON DELETE RESTRICT;
