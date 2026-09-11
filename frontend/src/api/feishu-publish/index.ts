import { get, post } from '../../utils/request'

// --- Types (match backend DTOs) ---

export interface FeishuPublishPathNode {
  node_token?: string
  title: string
}

export interface FeishuPublishCounts {
  create: number
  update: number
  move: number
  retire: number
  restore: number
  skip: number
  conflict: number
  blocked: number
  failed: number
  upload_bytes: number
}

export interface FeishuPublishPlanOp {
  op: string
  source_kind: string
  source_id: string
  title: string
  detail?: string
  blocked_by?: string
  upload_bytes?: number
}

export interface FeishuPublishItemResult {
  source_kind: string
  source_id: string
  op: string
  status: string
  error_code?: string
  summary?: string
}

export interface FeishuPublishRun {
  id: string
  tenant_id: number
  knowledge_base_id: string
  target_id: string
  config_id: string
  snapshot_id: string
  snapshot_digest: string
  space_id: string
  parent_node_token: string
  status: 'queued' | 'running' | 'partial' | 'succeeded' | 'failed' | string
  stage: string
  progress_done?: number
  progress_total?: number
  progress_label?: string
  config_revision?: unknown
  counts?: FeishuPublishCounts | null
  item_results?: FeishuPublishItemResult[] | null
  error_code?: string
  error_summary?: string
  feishu_home_url?: string
  started_at?: string | null
  finished_at?: string | null
  created_at: string
  updated_at: string
}

export interface FeishuPublishConfigView {
  configured: boolean
  app_id?: string
  app_secret_configured: boolean
  space_id?: string
  space_name?: string
  parent_node_token?: string
  parent_path?: FeishuPublishPathNode[]
  space_locked: boolean
  connection_status: 'unknown' | 'ok' | 'failed' | string
  last_connection_error?: string
  last_success_at?: string | null
  latest_run?: FeishuPublishRun | null
  hosted_summary?: FeishuPublishHostedSummary | null
  feature_enabled: boolean
}

export interface FeishuPublishHostedSummary {
  documents: number
  directories: number
  tags: number
  total_active: number
}

export interface FeishuPublishSpaceItem {
  space_id: string
  name: string
  description?: string
  visibility?: string
}

export interface FeishuPublishDiscoverSpacesRequest {
  app_id: string
  app_secret?: string
  page_token?: string
  page_size?: number
}

export interface FeishuPublishDiscoverSpacesResponse {
  items: FeishuPublishSpaceItem[]
  has_more: boolean
  page_token?: string
}

export interface FeishuPublishListNodesRequest {
  app_id: string
  app_secret?: string
  space_id: string
  parent_node_token?: string
  page_token?: string
  page_size?: number
}

export interface FeishuPublishNodeItem {
  node_token: string
  title: string
  has_child: boolean
  obj_type?: string
  path?: FeishuPublishPathNode[]
}

export interface FeishuPublishListNodesResponse {
  items: FeishuPublishNodeItem[]
  has_more: boolean
  page_token?: string
}

export interface FeishuPublishSyncRequest {
  app_id: string
  app_secret?: string
  space_id: string
  space_name?: string
  parent_node_token?: string
  parent_path?: FeishuPublishPathNode[]
  preview_digest?: string
  confirm_space_move?: boolean
}

export interface FeishuPublishPreview {
  digest: string
  counts: FeishuPublishCounts
  operations: FeishuPublishPlanOp[]
  warnings?: string[]
  space_id: string
  space_name: string
  parent_node_token: string
  parent_path?: FeishuPublishPathNode[]
}

export interface FeishuPublishConfirmResponse {
  accepted: boolean
  run?: FeishuPublishRun
  preview?: FeishuPublishPreview
  message?: string
}

export interface FeishuPublishError {
  code: string
  message: string
  hint?: string
}

function basePath(kbId: string) {
  return `/api/v1/knowledge-bases/${encodeURIComponent(kbId)}/feishu-publish`
}

export function getFeishuPublishConfig(kbId: string) {
  return get<FeishuPublishConfigView>(`${basePath(kbId)}/config`)
}

export function discoverFeishuSpaces(kbId: string, data: FeishuPublishDiscoverSpacesRequest) {
  return post<FeishuPublishDiscoverSpacesResponse>(`${basePath(kbId)}/spaces`, data)
}

export function listFeishuNodes(kbId: string, data: FeishuPublishListNodesRequest) {
  return post<FeishuPublishListNodesResponse>(`${basePath(kbId)}/nodes`, data)
}

export function previewFeishuPublish(kbId: string, data: FeishuPublishSyncRequest) {
  // Preview refreshes remote node state for all mappings; large KBs can exceed the default 30s.
  return post<FeishuPublishPreview>(`${basePath(kbId)}/preview`, data, { timeout: 180000 })
}

export function confirmFeishuPublish(kbId: string, data: FeishuPublishSyncRequest) {
  return post<FeishuPublishConfirmResponse>(`${basePath(kbId)}/confirm`, data, { timeout: 180000 })
}

export function unbindFeishuPublish(kbId: string) {
  return post<{ ok: boolean }>(`${basePath(kbId)}/unbind`, {})
}

export function getFeishuPublishRun(kbId: string, runId: string) {
  return get<FeishuPublishRun>(`${basePath(kbId)}/runs/${encodeURIComponent(runId)}`)
}
