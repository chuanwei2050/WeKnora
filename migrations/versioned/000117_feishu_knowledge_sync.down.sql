-- Migration: 000117_feishu_knowledge_sync (down)
DO $$ BEGIN RAISE NOTICE '[Migration 000117] Dropping feishu publish tables'; END $$;

DROP TABLE IF EXISTS feishu_publish_mappings;
DROP TABLE IF EXISTS feishu_publish_runs;
DROP TABLE IF EXISTS feishu_publish_snapshots;
DROP TABLE IF EXISTS feishu_publish_configs;

DO $$ BEGIN RAISE NOTICE '[Migration 000117] feishu publish tables dropped'; END $$;
