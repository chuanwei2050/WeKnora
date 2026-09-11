-- Migration: 000118_feishu_publish_run_progress
-- Description: Add live progress columns to feishu_publish_runs
DO $$ BEGIN RAISE NOTICE '[Migration 000118] Adding feishu publish run progress columns'; END $$;

ALTER TABLE feishu_publish_runs
    ADD COLUMN IF NOT EXISTS progress_done INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS progress_total INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS progress_label VARCHAR(512) NOT NULL DEFAULT '';
