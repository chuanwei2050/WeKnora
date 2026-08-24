ALTER TABLE integration_chat_bindings
    ADD COLUMN source VARCHAR(16) NOT NULL DEFAULT '';

CREATE INDEX idx_integration_chat_bindings_widget_subject
    ON integration_chat_bindings(client_id, tenant_id, user_id, source);
