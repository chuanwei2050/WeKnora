-- Migration: 000118_feishu_publish_run_progress (down)
ALTER TABLE feishu_publish_runs
    DROP COLUMN IF EXISTS progress_done,
    DROP COLUMN IF EXISTS progress_total,
    DROP COLUMN IF EXISTS progress_label;
