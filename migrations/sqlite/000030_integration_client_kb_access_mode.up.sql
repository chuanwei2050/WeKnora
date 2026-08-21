ALTER TABLE integration_clients ADD COLUMN knowledge_base_access_mode TEXT NOT NULL DEFAULT 'selected' CHECK (knowledge_base_access_mode IN ('selected', 'all'));
