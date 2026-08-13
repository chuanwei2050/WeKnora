ALTER TABLE messages ADD COLUMN IF NOT EXISTS response_timing JSONB NOT NULL DEFAULT '{}'::jsonb;
