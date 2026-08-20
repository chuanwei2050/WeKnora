ALTER TABLE graph_triple_candidates ADD COLUMN IF NOT EXISTS config_fingerprint VARCHAR(64) NOT NULL DEFAULT '';
