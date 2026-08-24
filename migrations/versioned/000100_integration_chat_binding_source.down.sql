DROP INDEX IF EXISTS idx_integration_chat_bindings_widget_subject;

ALTER TABLE integration_chat_bindings DROP COLUMN source;
