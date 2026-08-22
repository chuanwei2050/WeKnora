-- Add the wiki configuration and indexing strategy fields used by KnowledgeBase.
ALTER TABLE knowledge_bases ADD COLUMN wiki_config TEXT;
ALTER TABLE knowledge_bases ADD COLUMN indexing_strategy TEXT;

