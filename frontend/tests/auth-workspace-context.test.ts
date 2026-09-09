import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it } from 'vitest'

import { useAuthStore } from '../src/stores/auth'

describe('auth workspace context', () => {
  beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
  })

  it('clears tenant and knowledge-base state inherited from a previous login', () => {
    const store = useAuthStore()
    store.setSelectedTenant(10483, 'stale tenant')
    localStorage.setItem('weknora_knowledge_bases', '[{"id":"old-kb"}]')
    localStorage.setItem('weknora_current_kb', '{"id":"old-kb"}')

    store.resetWorkspaceContext()

    expect(store.selectedTenantId).toBeNull()
    expect(store.selectedTenantName).toBeNull()
    expect(store.knowledgeBases).toEqual([])
    expect(store.currentKnowledgeBase).toBeNull()
    expect(localStorage.getItem('weknora_selected_tenant_id')).toBeNull()
    expect(localStorage.getItem('weknora_selected_tenant_name')).toBeNull()
    expect(localStorage.getItem('weknora_knowledge_bases')).toBeNull()
    expect(localStorage.getItem('weknora_current_kb')).toBeNull()
  })
})
