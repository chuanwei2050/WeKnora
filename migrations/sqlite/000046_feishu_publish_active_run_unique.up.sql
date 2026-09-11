-- Migration: 000046_feishu_publish_active_run_unique
CREATE UNIQUE INDEX IF NOT EXISTS uq_feishu_publish_runs_active
	ON feishu_publish_runs (tenant_id, knowledge_base_id, target_id)
	WHERE status IN ('queued', 'running');
