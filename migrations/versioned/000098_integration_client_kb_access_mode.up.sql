ALTER TABLE integration_clients
    ADD COLUMN knowledge_base_access_mode VARCHAR(16) NOT NULL DEFAULT 'selected';

ALTER TABLE integration_clients
    ADD CONSTRAINT chk_integration_clients_kb_access_mode
    CHECK (knowledge_base_access_mode IN ('selected', 'all'));
