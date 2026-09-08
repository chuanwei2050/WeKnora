ALTER TABLE knowledges
    ADD COLUMN full_preview_status VARCHAR(32) NOT NULL DEFAULT 'none',
    ADD COLUMN full_preview_file_path TEXT NOT NULL DEFAULT '',
    ADD COLUMN full_preview_error TEXT NOT NULL DEFAULT '';
