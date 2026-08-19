<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import ChatView from '@/views/chat/index.vue'
import { createIntegrationChatSession, exchangeBootstrapTicket, getIntegrationChatSession, listIntegrationKnowledgeBases, refreshIntegrationSession } from '@/api/integration'
import { notifyEmbeddedHost, parseEmbeddedMessage } from '@/utils/embedded-runtime'

const authenticated = ref(false)
const sessionId = ref('')
const errorMessage = ref('')
const params = new URLSearchParams(window.location.search)
const allowedParentOrigin = window.location.origin
const agentId = computed(() => params.get('agent_id') || '')
const knowledgeBaseIds = ref<string[]>(params.getAll('knowledge_base_id'))
const selectionMode = ref<'selected' | 'all-allowed'>('selected')
const widgetMode = ref<'fixed' | 'selectable' | 'all-allowed'>('fixed')
const availableKnowledgeBases = ref<Array<{ id: string; name: string }>>([])
const instanceId = params.get('instance_id') || 'default'
const preserveSession = params.get('preserve_session') !== 'false'
const sessionStorageKey = `weknora-widget-chat:${instanceId}`
let refreshTimer: number | undefined

async function refreshAuthorizedKnowledgeBases() {
  const fresh = await listIntegrationKnowledgeBases()
  availableKnowledgeBases.value = fresh
  const allowed = new Set(fresh.map((kb) => kb.id))
  knowledgeBaseIds.value = knowledgeBaseIds.value.filter((id) => allowed.has(id))
}

async function onMessage(event: MessageEvent) {
  if (event.origin !== allowedParentOrigin || event.source !== window.parent) return
  const message = parseEmbeddedMessage(event.data)
  if (!message) return
  if (message.type === 'configure') {
    const configured = message.selection.mode === 'fixed' ? message.selection.knowledgeBaseIds : message.selection.initialKnowledgeBaseIds
    if (configured?.length) knowledgeBaseIds.value = configured
    selectionMode.value = message.selection.mode === 'all-allowed' ? 'all-allowed' : 'selected'
    widgetMode.value = message.selection.mode
    if (message.theme?.colorMode) document.documentElement.dataset.theme = message.theme.colorMode
    if (message.theme?.primaryColor) document.documentElement.style.setProperty('--td-brand-color', message.theme.primaryColor)
    if (message.theme?.title) document.title = message.theme.title
    return
  }
  if (message.type !== 'auth-ready') return
  if (authenticated.value) return
  try {
    const session = await exchangeBootstrapTicket(message.ticket)
    const configuredSelection = [...knowledgeBaseIds.value]
    knowledgeBaseIds.value = configuredSelection.length > 0
      ? configuredSelection
      : widgetMode.value === 'fixed'
        ? session.knowledge_base_ids
        : []
    if (widgetMode.value === 'selectable') {
      await refreshAuthorizedKnowledgeBases()
    }
    let chatSession: { id: string } | null = null
    const savedSessionId = preserveSession ? sessionStorage.getItem(sessionStorageKey) : null
    if (savedSessionId) {
      try { chatSession = await getIntegrationChatSession(savedSessionId) } catch { sessionStorage.removeItem(sessionStorageKey) }
    }
    chatSession ??= await createIntegrationChatSession(selectionMode.value === 'all-allowed'
      ? { mode: 'all-allowed' }
      : { mode: 'selected', knowledgeBaseIds: widgetMode.value === 'selectable' ? session.knowledge_base_ids : knowledgeBaseIds.value })
    sessionId.value = chatSession.id
    if (preserveSession) sessionStorage.setItem(sessionStorageKey, chatSession.id)
    authenticated.value = true
    if (refreshTimer !== undefined) window.clearInterval(refreshTimer)
    refreshTimer = window.setInterval(() => {
      refreshIntegrationSession().catch(() => notifyEmbeddedHost('unauthorized'))
    }, 10 * 60 * 1000)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '认证失败'
    notifyEmbeddedHost('unauthorized')
  }
}

onMounted(() => {
  window.addEventListener('message', onMessage)
  window.addEventListener('weknora:integration-authorization-changed', refreshAuthorizedKnowledgeBases)
  notifyEmbeddedHost('ready')
})
onBeforeUnmount(() => {
  window.removeEventListener('message', onMessage)
  window.removeEventListener('weknora:integration-authorization-changed', refreshAuthorizedKnowledgeBases)
  if (refreshTimer !== undefined) window.clearInterval(refreshTimer)
  if (!preserveSession) sessionStorage.removeItem(sessionStorageKey)
})
</script>

<template>
  <main class="embedded-widget">
    <label v-if="authenticated && widgetMode === 'selectable'" class="embedded-widget__selector">
      <span>回答知识库</span>
      <select v-model="knowledgeBaseIds" multiple aria-label="选择回答知识库">
        <option v-for="kb in availableKnowledgeBases" :key="kb.id" :value="kb.id">{{ kb.name }}</option>
      </select>
    </label>
    <ChatView v-if="authenticated && (widgetMode !== 'selectable' || knowledgeBaseIds.length > 0)" :session_id="sessionId" :agentId="agentId" :kbIds="selectionMode === 'all-allowed' ? [] : knowledgeBaseIds" :embeddedMode="true" />
    <div v-else-if="authenticated" class="embedded-widget__status" role="status">请至少选择一个知识库</div>
    <div v-else class="embedded-widget__status" role="status">
      {{ errorMessage || '正在等待宿主认证…' }}
    </div>
  </main>
</template>

<style scoped>
.embedded-widget { width: 100%; height: 100%; overflow: hidden; }
.embedded-widget__selector { display: flex; min-height: 44px; align-items: center; gap: 8px; padding: 6px 12px; border-bottom: 1px solid var(--td-component-border); }
.embedded-widget__selector select { min-width: 180px; max-height: 72px; }
.embedded-widget__status { display: grid; width: 100%; height: 100%; place-items: center; color: var(--td-text-color-secondary); }
</style>
