ALTER TABLE integration_clients
    ADD COLUMN administrator_user_id TEXT NOT NULL DEFAULT '';

CREATE TABLE integration_external_identities_next (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    client_id TEXT NOT NULL DEFAULT '',
    identity_provider_id TEXT NOT NULL,
    external_tenant_id TEXT NOT NULL,
    external_user_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    active INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    CONSTRAINT uq_integration_external_identity UNIQUE(client_id, external_tenant_id, external_user_id)
);

INSERT INTO integration_external_identities_next
    (id, client_id, identity_provider_id, external_tenant_id, external_user_id, user_id, active, created_at, updated_at)
SELECT id, '', identity_provider_id, external_tenant_id, external_user_id, user_id, 1, created_at, updated_at
FROM integration_external_identities;

DROP TABLE integration_external_identities;
ALTER TABLE integration_external_identities_next RENAME TO integration_external_identities;

CREATE INDEX idx_integration_external_identities_user_id
    ON integration_external_identities(user_id);
CREATE INDEX idx_integration_external_identities_client_active
    ON integration_external_identities(client_id, active);
