ALTER TABLE integration_clients
    ADD COLUMN IF NOT EXISTS secret_cipher TEXT NOT NULL DEFAULT '';
