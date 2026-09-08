ALTER TABLE knowledges ADD COLUMN preview_status TEXT NOT NULL DEFAULT 'none';
ALTER TABLE knowledges ADD COLUMN preview_file_path TEXT NOT NULL DEFAULT '';
ALTER TABLE knowledges ADD COLUMN preview_error TEXT NOT NULL DEFAULT '';
