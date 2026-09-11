-- Migration: 000045_feishu_publish_run_progress (down)
ALTER TABLE feishu_publish_runs DROP COLUMN progress_done;
ALTER TABLE feishu_publish_runs DROP COLUMN progress_total;
ALTER TABLE feishu_publish_runs DROP COLUMN progress_label;
