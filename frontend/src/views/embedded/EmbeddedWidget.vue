<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import ChatView from '@/views/chat/index.vue'
import { createIntegrationChatSession, exchangeBootstrapTicket, getIntegrationChatSession, listIntegrationChatSessions, listIntegrationKnowledgeBases, refreshIntegrationSession, type IntegrationChatSession } from '@/api/integration'
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
const conversations = ref<IntegrationChatSession[]>([])
const conversationsOpen = ref(false)
const instanceId = params.get('instance_id') || 'default'
const preserveSession = params.get('preserve_session') !== 'false'
const sessionStorageKey = `weknora-widget-chat:${instanceId}`
let refreshTimer: number | undefined

async function createChatSession() {
  const chatSession = await createIntegrationChatSession(selectionMode.value === 'all-allowed'
    ? { mode: 'all-allowed' }
    : { mode: 'selected', knowledgeBaseIds: knowledgeBaseIds.value })
  sessionId.value = chatSession.id
  if (preserveSession) sessionStorage.setItem(sessionStorageKey, chatSession.id)
  refreshConversations().catch(() => undefined)
}

async function refreshConversations() {
  conversations.value = await listIntegrationChatSessions()
}

function switchConversation(id: string) {
  sessionId.value = id
  conversationsOpen.value = false
  if (preserveSession) sessionStorage.setItem(sessionStorageKey, id)
}

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
  if (message.type === 'toggle-conversations') {
    if (!authenticated.value) return
    conversationsOpen.value = !conversationsOpen.value
    if (conversationsOpen.value) await refreshConversations()
    return
  }
  if (message.type === 'new-conversation') {
    if (!authenticated.value) return
    try { await createChatSession() } catch (error) {
      errorMessage.value = error instanceof Error ? error.message : '创建会话失败'
    }
    return
  }
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
    if (chatSession) {
      sessionId.value = chatSession.id
    } else {
      await createChatSession()
    }
    authenticated.value = true
    await refreshConversations()
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
    <aside v-if="authenticated && conversationsOpen" class="embedded-widget__conversations" aria-label="对话列表">
      <div class="embedded-widget__conversations-heading">最近对话</div>
      <button v-for="(conversation, index) in conversations" :key="conversation.id" type="button" :class="{ active: conversation.id === sessionId }" @click="switchConversation(conversation.id)">
        <span>{{ conversation.title || `新对话 ${conversations.length - index}` }}</span>
        <time>{{ new Date(conversation.updated_at || conversation.created_at).toLocaleString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' }) }}</time>
      </button>
      <div v-if="conversations.length === 0" class="embedded-widget__conversations-empty">暂无对话</div>
    </aside>
    <label v-if="authenticated && widgetMode === 'selectable'" class="embedded-widget__selector">
      <span>回答知识库</span>
      <select v-model="knowledgeBaseIds" multiple aria-label="选择回答知识库">
        <option v-for="kb in availableKnowledgeBases" :key="kb.id" :value="kb.id">{{ kb.name }}</option>
      </select>
    </label>
    <ChatView v-if="authenticated && (widgetMode !== 'selectable' || knowledgeBaseIds.length > 0)" :key="sessionId" :session_id="sessionId" :agentId="agentId" :kbIds="selectionMode === 'all-allowed' ? [] : knowledgeBaseIds" :embeddedMode="true" />
    <div v-else-if="authenticated" class="embedded-widget__status" role="status">请至少选择一个知识库</div>
    <div v-else class="embedded-widget__status" role="status">
      {{ errorMessage || '正在等待宿主认证…' }}
    </div>
  </main>
</template>

<style scoped>
.embedded-widget { position: relative; display: flex; width: 100%; height: 100%; flex-direction: column; overflow: hidden; background: #f7f9fc; }
.embedded-widget__conversations { position: absolute; z-index: 5; inset: 0 auto 0 0; width: min(82%, 300px); padding: 14px 10px; overflow-y: auto; border-right: 1px solid #dce4e8; background: rgba(255, 255, 255, .98); box-shadow: 12px 0 30px rgba(19, 42, 52, .12); }
.embedded-widget__conversations-heading { padding: 4px 10px 12px; color: #61727a; font-size: 12px; font-weight: 600; letter-spacing: .04em; }
.embedded-widget__conversations button { display: flex; width: 100%; flex-direction: column; gap: 4px; padding: 10px; border: 0; border-radius: 10px; color: #20343d; background: transparent; text-align: left; cursor: pointer; }
.embedded-widget__conversations button:hover, .embedded-widget__conversations button.active { background: #eaf3f6; }
.embedded-widget__conversations button span { width: 100%; overflow: hidden; font-size: 14px; font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }
.embedded-widget__conversations time, .embedded-widget__conversations-empty { color: #7b8d95; font-size: 12px; }
.embedded-widget__conversations-empty { padding: 20px 10px; text-align: center; }
.embedded-widget__selector { display: flex; min-height: 44px; align-items: center; gap: 8px; padding: 6px 12px; border-bottom: 1px solid var(--td-component-border); }
.embedded-widget__selector select { min-width: 180px; max-height: 72px; }
.embedded-widget__status { display: grid; width: 100%; height: 100%; place-items: center; color: var(--td-text-color-secondary); }
</style>
