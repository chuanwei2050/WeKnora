<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import ChatView from '@/views/chat/index.vue'
import { BUILTIN_QUICK_ANSWER_ID } from '@/api/agent'
import { createIntegrationChatSession, deleteIntegrationChatSession, exchangeBootstrapTicket, getIntegrationChatSession, listIntegrationChatSessions, listIntegrationFrequentQuestions, listIntegrationKnowledgeBases, refreshIntegrationSession, renameIntegrationChatSession, type IntegrationChatSession } from '@/api/integration'
import { clearEmbeddedAuth, isIntegrationAuthFailure, notifyEmbeddedHost, parseEmbeddedMessage, resolveEmbeddedParentOrigin } from '@/utils/embedded-runtime'

const authenticated = ref(false)
const sessionId = ref('')
const errorMessage = ref('')
const noticeMessage = ref('')
const params = new URLSearchParams(window.location.search)
const configuredParentOrigin = params.get('parent_origin')
const allowedParentOrigin = resolveEmbeddedParentOrigin(configuredParentOrigin, document.referrer, window.location.origin)
const widgetAgentId = BUILTIN_QUICK_ANSWER_ID
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
const frequentQuestions = ref<string[]>([])
const compatibleConversations = computed(() => conversations.value.filter(isCompatibleConversation))
const conversationsOpen = ref(false)
const editingConversationId = ref('')
const editingTitle = ref('')
const instanceId = params.get('instance_id') || 'default'
const preserveSession = params.get('preserve_session') !== 'false'
const sessionStorageKey = `weknora-widget-chat:${instanceId}`
let refreshTimer: number | undefined
let readyTimer: number | undefined
let authenticating = false
let noticeTimer: number | undefined

function showNotice(message: string) {
  noticeMessage.value = message
  if (noticeTimer !== undefined) window.clearTimeout(noticeTimer)
  noticeTimer = window.setTimeout(() => { noticeMessage.value = '' }, 1600)
}

function expireAuthentication() {
  if (!authenticated.value) return
  authenticated.value = false
  clearEmbeddedAuth()
  if (refreshTimer !== undefined) {
    window.clearInterval(refreshTimer)
    refreshTimer = undefined
  }
  notifyEmbeddedHost('unauthorized')
}

async function createChatSession() {
  const sessionKnowledgeBaseIds = widgetMode.value === 'selectable'
    ? availableKnowledgeBases.value.map((kb) => kb.id)
    : knowledgeBaseIds.value
  const chatSession = await createIntegrationChatSession(selectionMode.value === 'all-allowed'
    ? { mode: 'all-allowed' }
    : { mode: 'selected', knowledgeBaseIds: sessionKnowledgeBaseIds })
  sessionId.value = chatSession.id
  showNotice('已新建对话')
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

function beginRename(conversation: IntegrationChatSession) {
  editingConversationId.value = conversation.id
  editingTitle.value = conversation.title || fallbackConversationTitle(conversation)
}

function fallbackConversationTitle(conversation: IntegrationChatSession) {
  const createdAt = new Date(conversation.created_at)
  if (Number.isNaN(createdAt.getTime())) return '新对话'
  return `新对话 · ${createdAt.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })}`
}

async function saveRename(conversation: IntegrationChatSession) {
  const title = editingTitle.value.trim()
  if (!title) return
  try {
    await renameIntegrationChatSession(conversation.id, title)
    editingConversationId.value = ''
    await refreshConversations()
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '重命名失败'
  }
}

function cancelRename() {
  editingConversationId.value = ''
  editingTitle.value = ''
}

async function deleteConversation(conversation: IntegrationChatSession) {
  try {
    const deletingActiveConversation = conversation.id === sessionId.value
    await deleteIntegrationChatSession(conversation.id)
    await refreshConversations()
    if (deletingActiveConversation) {
      const nextConversation = compatibleConversations.value[0]
      if (nextConversation) {
        sessionId.value = nextConversation.id
        if (preserveSession) sessionStorage.setItem(sessionStorageKey, nextConversation.id)
      } else {
        await createChatSession()
      }
    }
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '删除会话失败'
  }
}

async function refreshConversations() {
  conversations.value = await listIntegrationChatSessions()
}

async function refreshFrequentQuestions() {
  const questions = await listIntegrationFrequentQuestions()
  frequentQuestions.value = questions.slice(0, 3)
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
    await refreshConversations()
    chatSession ??= compatibleConversations.value.find((conversation) => !conversation.title.trim()) ?? null
    if (chatSession) {
      sessionId.value = chatSession.id
      if (preserveSession) sessionStorage.setItem(sessionStorageKey, chatSession.id)
    } else {
      await createChatSession()
    }
    authenticated.value = true
    if (readyTimer !== undefined) window.clearInterval(readyTimer)
    await refreshFrequentQuestions().catch(() => { frequentQuestions.value = [] })
    if (refreshTimer !== undefined) window.clearInterval(refreshTimer)
    refreshTimer = window.setInterval(() => {
      refreshIntegrationSession().catch((error) => {
        if (isIntegrationAuthFailure(error)) expireAuthentication()
      })
    }, 10 * 60 * 1000)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '认证失败'
    if (isIntegrationAuthFailure(error)) {
      clearEmbeddedAuth()
      notifyEmbeddedHost('unauthorized')
    }
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
  if (noticeTimer !== undefined) window.clearTimeout(noticeTimer)
  if (!preserveSession) sessionStorage.removeItem(sessionStorageKey)
})
</script>

<template>
  <main class="embedded-widget">
    <div v-if="noticeMessage" class="embedded-widget__notice" role="status">{{ noticeMessage }}</div>
    <button v-if="authenticated && conversationsOpen" class="embedded-widget__backdrop" type="button" aria-label="关闭对话列表" @click="conversationsOpen = false" />
    <aside v-if="authenticated && conversationsOpen" class="embedded-widget__conversations" aria-label="对话列表">
      <div class="embedded-widget__conversations-heading">
        <strong>最近对话</strong>
        <span>
          <button type="button" aria-label="新建对话" @click="createConversationFromDrawer">＋</button>
          <button type="button" aria-label="关闭对话列表" @click="conversationsOpen = false">×</button>
        </span>
      </div>
      <div v-for="conversation in compatibleConversations" :key="conversation.id" class="embedded-widget__conversation" :data-session-id="conversation.id" :class="{ active: conversation.id === sessionId }" @click="switchConversation(conversation)">
        <input v-if="editingConversationId === conversation.id" v-model="editingTitle" maxlength="100" aria-label="对话标题" @click.stop @keydown.enter.prevent.stop="saveRename(conversation)" @keydown.esc.prevent.stop="cancelRename" />
        <span v-else :title="conversation.title || fallbackConversationTitle(conversation)">{{ conversation.title || fallbackConversationTitle(conversation) }}</span>
        <time>{{ new Date(conversation.updated_at || conversation.created_at).toLocaleString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' }) }}</time>
        <span class="embedded-widget__conversation-actions">
          <template v-if="editingConversationId === conversation.id">
            <button type="button" aria-label="保存对话名称" title="保存" @click.stop="saveRename(conversation)">✓</button>
            <button type="button" aria-label="取消重命名" title="取消" @click.stop="cancelRename">×</button>
          </template>
          <template v-else>
            <button type="button" aria-label="重命名对话" title="重命名" @click.stop="beginRename(conversation)"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m4 16-.8 4.8L8 20l11-11-4-4L4 16Zm9.5-9.5 4 4" /></svg></button>
            <t-popconfirm content="确认删除此对话？" placement="right" :confirm-btn="{ content: '删除', theme: 'danger' }" :cancel-btn="{ content: '取消' }" @confirm="deleteConversation(conversation)">
              <button type="button" aria-label="删除对话" title="删除" @click.stop><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h16M9 7V4h6v3m3 0-1 13H7L6 7m4 4v5m4-5v5" /></svg></button>
            </t-popconfirm>
          </template>
        </span>
      </div>
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
    <ChatView v-if="authenticated && selectionReady" :key="sessionId" :session_id="sessionId" :agentId="widgetAgentId" :kbIds="selectionMode === 'all-allowed' ? [] : knowledgeBaseIds" :embeddedMode="true" :embeddedSuggestedQuestions="frequentQuestions" :suggestedQuestionsEnabled="false" @answer-completed="refreshConversations" />
    <div v-else-if="authenticated" class="embedded-widget__status" role="status">请至少选择一个知识库</div>
    <div v-else class="embedded-widget__status" role="status">
      {{ errorMessage || '正在等待宿主认证…' }}
    </div>
  </main>
</template>

<style scoped>
.embedded-widget { position: relative; display: flex; width: 100%; height: 100%; min-width: 0; min-height: 0; flex-direction: column; overflow: hidden; background: #f4f8fb; }
.embedded-widget > * { min-width: 0; min-height: 0; }
.embedded-widget__notice { position: absolute; z-index: 12; top: 12px; left: 50%; padding: 7px 12px; transform: translateX(-50%); border: 1px solid #b8ddea; border-radius: 999px; color: #155f7d; background: rgba(240, 250, 253, .96); box-shadow: 0 8px 24px rgba(22, 74, 96, .14); font-size: 12px; white-space: nowrap; }
.embedded-widget__backdrop { position: absolute; z-index: 4; inset: 0; border: 0; background: rgba(19, 42, 52, .12); }
.embedded-widget__conversations { position: absolute; z-index: 5; box-sizing: border-box; inset: 0 auto 0 0; width: min(82%, 300px); padding: 14px 10px; overflow-x: hidden; overflow-y: auto; border-right: 1px solid #dce4e8; background: rgba(255, 255, 255, .98); box-shadow: 12px 0 30px rgba(19, 42, 52, .12); }
.embedded-widget__conversations-heading { display: flex; align-items: center; justify-content: space-between; padding: 2px 8px 12px; color: #263a45; font-size: 13px; }
.embedded-widget__conversations-heading span { display: flex; gap: 6px; }
.embedded-widget__conversations-heading button { display: grid; width: 28px; height: 28px; padding: 0; place-items: center; border: 0; border-radius: 8px; color: #42606e; background: #edf5f8; font-size: 17px; line-height: 1; }
.embedded-widget__conversation { position: relative; display: grid; box-sizing: border-box; width: 100%; min-width: 0; gap: 4px; padding: 10px 66px 10px 10px; overflow: hidden; border-radius: 10px; color: #20343d; text-align: left; cursor: pointer; }
.embedded-widget__conversation:hover, .embedded-widget__conversation.active { background: #eaf3f6; }
.embedded-widget__conversation > span:first-child { width: 100%; overflow: hidden; font-size: 14px; font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }
.embedded-widget__conversation input { box-sizing: border-box; width: 100%; min-width: 0; border: 1px solid #8fc8dd; border-radius: 6px; padding: 4px 6px; outline: none; }
.embedded-widget__conversation-actions { position: absolute; top: 9px; right: 8px; display: flex; gap: 4px; opacity: .78; }
.embedded-widget__conversation:hover .embedded-widget__conversation-actions, .embedded-widget__conversation:focus-within .embedded-widget__conversation-actions { opacity: 1; }
.embedded-widget__conversation-actions button { display: grid; width: 25px; height: 25px; place-items: center; border: 0; border-radius: 7px; color: #526b76; background: #fff; cursor: pointer; }
.embedded-widget__conversation-actions button.danger { color: #b42318; background: #fff0ee; }
.embedded-widget__conversation-actions svg { width: 14px; height: 14px; fill: none; stroke: currentColor; stroke-width: 1.8; stroke-linecap: round; stroke-linejoin: round; }
.embedded-widget__conversations time, .embedded-widget__conversations-empty { color: #7b8d95; font-size: 12px; }
.embedded-widget__conversations-empty { padding: 20px 10px; text-align: center; }
.embedded-widget__scope { position: relative; z-index: 3; display: flex; min-height: 44px; max-height: 92px; flex: 0 0 auto; flex-wrap: wrap; align-items: center; gap: 7px; padding: 6px 12px; overflow-y: auto; overflow-x: hidden; border-bottom: 1px solid #dce8ed; background: #fff; }
.embedded-widget__scope-label { flex: 0 0 auto; color: #6a7d87; font-size: 12px; }
.embedded-widget__scope-chip, .embedded-widget__scope-picker summary { display: block; max-width: min(210px, calc(100vw - 96px)); overflow: hidden; padding: 5px 9px; border-radius: 8px; color: #185f7d; background: #e9f7fb; font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.embedded-widget__scope-picker { position: relative; flex: 0 0 auto; }
.embedded-widget__scope-picker summary { cursor: pointer; list-style: none; }
.embedded-widget__scope-picker summary::-webkit-details-marker { display: none; }
.embedded-widget__scope-options { position: fixed; z-index: 8; top: 48px; left: 12px; width: min(300px, calc(100vw - 24px)); max-height: 220px; padding: 7px; overflow-y: auto; border: 1px solid #d7e5eb; border-radius: 12px; background: #fff; box-shadow: 0 14px 36px rgba(19, 42, 52, .18); }
.embedded-widget__scope-options label { display: flex; align-items: center; gap: 8px; padding: 8px; border-radius: 8px; color: #263a45; font-size: 13px; cursor: pointer; }
.embedded-widget__scope-options label:hover { background: #eef7fa; }
.embedded-widget__status { display: grid; width: 100%; height: 100%; place-items: center; color: var(--td-text-color-secondary); }
</style>
