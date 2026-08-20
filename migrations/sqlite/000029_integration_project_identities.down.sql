CREATE TABLE integration_external_identities_previous (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    identity_provider_id TEXT NOT NULL,
    external_tenant_id TEXT NOT NULL,
    external_user_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    CONSTRAINT uq_integration_external_identity UNIQUE(identity_provider_id, external_tenant_id, external_user_id)
);

INSERT OR IGNORE INTO integration_external_identities_previous
    (id, identity_provider_id, external_tenant_id, external_user_id, user_id, created_at, updated_at)
SELECT id, identity_provider_id, external_tenant_id, external_user_id, user_id, created_at, updated_at
FROM integration_external_identities;

DROP TABLE integration_external_identities;
ALTER TABLE integration_external_identities_previous RENAME TO integration_external_identities;

CREATE INDEX idx_integration_external_identities_user_id
    ON integration_external_identities(user_id);

ALTER TABLE integration_clients DROP COLUMN administrator_user_id;
