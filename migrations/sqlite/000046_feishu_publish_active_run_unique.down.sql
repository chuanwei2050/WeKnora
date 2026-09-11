-- Migration: 000046_feishu_publish_active_run_unique (down)
DROP INDEX IF EXISTS uq_feishu_publish_runs_active;
