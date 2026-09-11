-- Migration: 000045_feishu_publish_run_progress
ALTER TABLE feishu_publish_runs ADD COLUMN progress_done INTEGER NOT NULL DEFAULT 0;
ALTER TABLE feishu_publish_runs ADD COLUMN progress_total INTEGER NOT NULL DEFAULT 0;
ALTER TABLE feishu_publish_runs ADD COLUMN progress_label TEXT NOT NULL DEFAULT '';
