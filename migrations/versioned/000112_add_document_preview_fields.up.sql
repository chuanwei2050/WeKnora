ALTER TABLE knowledges
    ADD COLUMN preview_status VARCHAR(32) NOT NULL DEFAULT 'none',
    ADD COLUMN preview_file_path TEXT NOT NULL DEFAULT '',
    ADD COLUMN preview_error TEXT NOT NULL DEFAULT '';
