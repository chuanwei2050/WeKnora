const WORKSPACE_CONTEXT_KEYS = [
  'weknora_knowledge_bases',
  'weknora_current_kb',
  'weknora_selected_tenant_id',
  'weknora_selected_tenant_name',
] as const

export function clearStoredWorkspaceContext(): void {
  for (const key of WORKSPACE_CONTEXT_KEYS) localStorage.removeItem(key)
}
