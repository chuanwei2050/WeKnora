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
const selectedKnowledgeBases = computed(() => {
  const names = new Map(availableKnowledgeBases.value.map((kb) => [kb.id, kb.name]))
  return knowledgeBaseIds.value.map((id) => ({ id, name: names.get(id) || id }))
})
const selectionReady = computed(() => selectionMode.value === 'all-allowed' || knowledgeBaseIds.value.length > 0)
const conversations = ref<IntegrationChatSession[]>([])
const compatibleConversations = computed(() => conversations.value.filter(isCompatibleConversation))
const conversationsOpen = ref(false)
const instanceId = params.get('instance_id') || 'default'
const preserveSession = params.get('preserve_session') !== 'false'
const sessionStorageKey = `weknora-widget-chat:${instanceId}`
let refreshTimer: number | undefined
let readyTimer: number | undefined
let authenticating = false

async function createChatSession() {
  const sessionKnowledgeBaseIds = widgetMode.value === 'selectable'
    ? availableKnowledgeBases.value.map((kb) => kb.id)
    : knowledgeBaseIds.value
  const chatSession = await createIntegrationChatSession(selectionMode.value === 'all-allowed'
    ? { mode: 'all-allowed' }
    : { mode: 'selected', knowledgeBaseIds: sessionKnowledgeBaseIds })
  sessionId.value = chatSession.id
  if (preserveSession) sessionStorage.setItem(sessionStorageKey, chatSession.id)
  refreshConversations().catch(() => undefined)
}

async function createConversationFromDrawer() {
  try {
    await createChatSession()
    conversationsOpen.value = false
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '创建会话失败'
  }
}

async function refreshConversations() {
  conversations.value = await listIntegrationChatSessions()
}

function isCompatibleConversation(conversation: IntegrationChatSession) {
  if (widgetMode.value === 'all-allowed') return conversation.knowledge_base_mode === 'all-allowed'
  if (conversation.knowledge_base_mode !== 'selected') return false
  if (widgetMode.value === 'fixed') {
    const allowed = new Set(conversation.allowed_knowledge_base_ids)
    return knowledgeBaseIds.value.every((id) => allowed.has(id))
  }
  return conversation.allowed_knowledge_base_ids.length > 0
}

function switchConversation(conversation: IntegrationChatSession) {
  if (!isCompatibleConversation(conversation)) return
  if (widgetMode.value === 'selectable') {
    const allowed = new Set(conversation.allowed_knowledge_base_ids)
    knowledgeBaseIds.value = knowledgeBaseIds.value.filter((id) => allowed.has(id))
  }
  sessionId.value = conversation.id
  conversationsOpen.value = false
  if (preserveSession) sessionStorage.setItem(sessionStorageKey, conversation.id)
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
  if (authenticated.value || authenticating) return
  authenticating = true
  try {
    const session = await exchangeBootstrapTicket(message.ticket)
    const configuredSelection = [...knowledgeBaseIds.value]
    knowledgeBaseIds.value = configuredSelection.length > 0
      ? configuredSelection
      : widgetMode.value === 'fixed'
        ? session.knowledge_base_ids
        : []
    if (widgetMode.value === 'selectable') await refreshAuthorizedKnowledgeBases()
    else await refreshAuthorizedKnowledgeBases().catch(() => { availableKnowledgeBases.value = [] })
    let chatSession: IntegrationChatSession | null = null
    const savedSessionId = preserveSession ? sessionStorage.getItem(sessionStorageKey) : null
    if (savedSessionId) {
      try {
        const savedSession = await getIntegrationChatSession(savedSessionId)
        if (isCompatibleConversation(savedSession)) chatSession = savedSession
        else sessionStorage.removeItem(sessionStorageKey)
      } catch { sessionStorage.removeItem(sessionStorageKey) }
    }
    if (chatSession) {
      sessionId.value = chatSession.id
    } else {
      await createChatSession()
    }
    authenticated.value = true
    if (readyTimer !== undefined) window.clearInterval(readyTimer)
    await refreshConversations()
    if (refreshTimer !== undefined) window.clearInterval(refreshTimer)
    refreshTimer = window.setInterval(() => {
      refreshIntegrationSession().catch(() => notifyEmbeddedHost('unauthorized'))
    }, 10 * 60 * 1000)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '认证失败'
    notifyEmbeddedHost('unauthorized')
  } finally {
    authenticating = false
  }
}

onMounted(() => {
  window.addEventListener('message', onMessage)
  window.addEventListener('weknora:integration-authorization-changed', refreshAuthorizedKnowledgeBases)
  notifyEmbeddedHost('ready')
  readyTimer = window.setInterval(() => {
    if (!authenticated.value) notifyEmbeddedHost('ready')
  }, 1_500)
})
onBeforeUnmount(() => {
  window.removeEventListener('message', onMessage)
  window.removeEventListener('weknora:integration-authorization-changed', refreshAuthorizedKnowledgeBases)
  if (refreshTimer !== undefined) window.clearInterval(refreshTimer)
  if (readyTimer !== undefined) window.clearInterval(readyTimer)
  if (!preserveSession) sessionStorage.removeItem(sessionStorageKey)
})
</script>

<template>
  <main class="embedded-widget">
    <button v-if="authenticated && conversationsOpen" class="embedded-widget__backdrop" type="button" aria-label="关闭对话列表" @click="conversationsOpen = false" />
    <aside v-if="authenticated && conversationsOpen" class="embedded-widget__conversations" aria-label="对话列表">
      <div class="embedded-widget__conversations-heading">
        <strong>最近对话</strong>
        <span>
          <button type="button" aria-label="新建对话" @click="createConversationFromDrawer">＋</button>
          <button type="button" aria-label="关闭对话列表" @click="conversationsOpen = false">×</button>
        </span>
      </div>
      <button v-for="(conversation, index) in compatibleConversations" :key="conversation.id" type="button" :class="{ active: conversation.id === sessionId }" @click="switchConversation(conversation)">
        <span>{{ conversation.title || `新对话 ${compatibleConversations.length - index}` }}</span>
        <time>{{ new Date(conversation.updated_at || conversation.created_at).toLocaleString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' }) }}</time>
      </button>
      <div v-if="compatibleConversations.length === 0" class="embedded-widget__conversations-empty">暂无对话</div>
    </aside>
    <section v-if="authenticated" class="embedded-widget__scope" aria-label="当前回答范围">
      <span class="embedded-widget__scope-label">回答范围</span>
      <details v-if="widgetMode === 'selectable'" class="embedded-widget__scope-picker">
        <summary>{{ knowledgeBaseIds.length ? `已选 ${knowledgeBaseIds.length} 个知识库` : '请选择知识库' }}</summary>
        <div class="embedded-widget__scope-options">
          <label v-for="kb in availableKnowledgeBases" :key="kb.id">
            <input v-model="knowledgeBaseIds" type="checkbox" :value="kb.id" />
            <span>{{ kb.name }}</span>
          </label>
        </div>
      </details>
      <span v-else-if="widgetMode === 'all-allowed'" class="embedded-widget__scope-chip">全部授权知识库（{{ availableKnowledgeBases.length }}）</span>
      <span v-for="kb in selectedKnowledgeBases" v-else :key="kb.id" class="embedded-widget__scope-chip" :title="kb.name">{{ kb.name }}</span>
    </section>
    <ChatView v-if="authenticated && selectionReady" :key="sessionId" :session_id="sessionId" :agentId="agentId" :kbIds="selectionMode === 'all-allowed' ? [] : knowledgeBaseIds" :embeddedMode="true" @answer-completed="refreshConversations" />
    <div v-else-if="authenticated" class="embedded-widget__status" role="status">请至少选择一个知识库</div>
    <div v-else class="embedded-widget__status" role="status">
      {{ errorMessage || '正在等待宿主认证…' }}
    </div>
  </main>
</template>

<style scoped>
.embedded-widget { position: relative; display: flex; width: 100%; height: 100%; min-width: 0; min-height: 0; flex-direction: column; overflow: hidden; background: #f4f8fb; }
.embedded-widget > * { min-width: 0; min-height: 0; }
.embedded-widget__backdrop { position: absolute; z-index: 4; inset: 0; border: 0; background: rgba(19, 42, 52, .12); }
.embedded-widget__conversations { position: absolute; z-index: 5; inset: 0 auto 0 0; width: min(82%, 300px); padding: 14px 10px; overflow-y: auto; border-right: 1px solid #dce4e8; background: rgba(255, 255, 255, .98); box-shadow: 12px 0 30px rgba(19, 42, 52, .12); }
.embedded-widget__conversations-heading { display: flex; align-items: center; justify-content: space-between; padding: 2px 8px 12px; color: #263a45; font-size: 13px; }
.embedded-widget__conversations-heading span { display: flex; gap: 6px; }
.embedded-widget__conversations-heading button { display: grid; width: 28px; height: 28px; padding: 0; place-items: center; border: 0; border-radius: 8px; color: #42606e; background: #edf5f8; font-size: 17px; line-height: 1; }
.embedded-widget__conversations > button { display: flex; width: 100%; flex-direction: column; gap: 4px; padding: 10px; border: 0; border-radius: 10px; color: #20343d; background: transparent; text-align: left; cursor: pointer; }
.embedded-widget__conversations > button:hover, .embedded-widget__conversations > button.active { background: #eaf3f6; }
.embedded-widget__conversations > button span { width: 100%; overflow: hidden; font-size: 14px; font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }
.embedded-widget__conversations time, .embedded-widget__conversations-empty { color: #7b8d95; font-size: 12px; }
.embedded-widget__conversations-empty { padding: 20px 10px; text-align: center; }
.embedded-widget__scope { position: relative; z-index: 3; display: flex; min-height: 44px; flex: 0 0 auto; align-items: center; gap: 7px; padding: 6px 12px; overflow-x: auto; border-bottom: 1px solid #dce8ed; background: #fff; scrollbar-width: none; }
.embedded-widget__scope-label { flex: 0 0 auto; color: #6a7d87; font-size: 12px; }
.embedded-widget__scope-chip, .embedded-widget__scope-picker summary { display: block; max-width: 210px; overflow: hidden; padding: 5px 9px; border-radius: 8px; color: #185f7d; background: #e9f7fb; font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.embedded-widget__scope-picker { position: relative; flex: 0 0 auto; }
.embedded-widget__scope-picker summary { cursor: pointer; list-style: none; }
.embedded-widget__scope-picker summary::-webkit-details-marker { display: none; }
.embedded-widget__scope-options { position: fixed; z-index: 8; top: 48px; left: 12px; width: min(300px, calc(100vw - 24px)); max-height: 220px; padding: 7px; overflow-y: auto; border: 1px solid #d7e5eb; border-radius: 12px; background: #fff; box-shadow: 0 14px 36px rgba(19, 42, 52, .18); }
.embedded-widget__scope-options label { display: flex; align-items: center; gap: 8px; padding: 8px; border-radius: 8px; color: #263a45; font-size: 13px; cursor: pointer; }
.embedded-widget__scope-options label:hover { background: #eef7fa; }
.embedded-widget__status { display: grid; width: 100%; height: 100%; place-items: center; color: var(--td-text-color-secondary); }
</style>
