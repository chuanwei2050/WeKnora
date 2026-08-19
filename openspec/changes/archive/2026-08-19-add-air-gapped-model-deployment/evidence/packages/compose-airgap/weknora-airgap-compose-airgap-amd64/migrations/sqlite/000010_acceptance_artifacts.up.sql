CREATE TABLE IF NOT EXISTS acceptance_artifacts (id TEXT PRIMARY KEY, run_id TEXT NOT NULL, uri TEXT NOT NULL, sha256 TEXT NOT NULL, size_bytes INTEGER NOT NULL, content_type TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, FOREIGN KEY (run_id) REFERENCES acceptance_runs(id));
CREATE INDEX IF NOT EXISTS idx_acceptance_artifacts_run ON acceptance_artifacts(run_id, created_at);
