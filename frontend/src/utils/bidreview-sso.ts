type BidReviewSession = {
  user?: unknown
  tenant?: unknown
  token?: string
  refresh_token?: string
  bidreview_role?: string
  default_knowledge_base?: BidReviewDefaultKnowledgeBase
  default_agent?: BidReviewDefaultAgent
}

type BidReviewDefaultKnowledgeBase = {
  id?: string
  name?: string
  description?: string
  source?: string
}

type BidReviewDefaultAgent = {
  id?: string
  name?: string
  source?: string
}

const BIDREVIEW_TOKEN_KEY = 'bidreview_access_token'
const BIDREVIEW_RETURN_PATH_KEY = 'bidreview_knowledge_return_path'
const BIDREVIEW_EMBEDDED_MODE_KEY = 'weknora_bidreview_embedded'
const BIDREVIEW_ROLE_KEY = 'weknora_bidreview_role'

function safeBidReviewReturnPath() {
  const fallback = '/projects'
  const saved = sessionStorage.getItem(BIDREVIEW_RETURN_PATH_KEY)
  if (!saved) return fallback
  if (!saved.startsWith('/') || saved.startsWith('//') || saved.startsWith('/knowledge')) return fallback
  return saved
}

export function isBidReviewEmbeddedMode(): boolean {
  return (
    window.location.pathname.startsWith('/knowledge') ||
    localStorage.getItem(BIDREVIEW_EMBEDDED_MODE_KEY) === 'true'
  )
}

export function canManageBidReviewKnowledge(): boolean {
  if (!isBidReviewEmbeddedMode()) return true
  const role = localStorage.getItem(BIDREVIEW_ROLE_KEY)
  return role === 'tenant_admin' || role === 'platform_admin'
}

export function returnToBidReview(): void {
  window.location.assign(safeBidReviewReturnPath())
}

export async function ensureBidReviewSession(): Promise<void> {
  if (!window.location.pathname.startsWith('/knowledge')) return
  localStorage.setItem(BIDREVIEW_EMBEDDED_MODE_KEY, 'true')
  localStorage.setItem('weknora_lite_mode', 'true')

  const bidReviewToken = sessionStorage.getItem(BIDREVIEW_TOKEN_KEY)
  if (!bidReviewToken) {
    window.location.replace('/')
    return
  }

  const response = await fetch('/api/knowledge/weknora/session', {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${bidReviewToken}`,
      'Content-Type': 'application/json',
    },
  })
  if (!response.ok) {
    window.location.replace('/')
    return
  }

  const session = (await response.json()) as BidReviewSession
  if (!session.token || !session.refresh_token || !session.user || !session.tenant) {
    window.location.replace('/')
    return
  }

  localStorage.setItem('weknora_user', JSON.stringify(session.user))
  localStorage.setItem('weknora_tenant', JSON.stringify(session.tenant))
  localStorage.setItem('weknora_token', session.token)
  localStorage.setItem('weknora_refresh_token', session.refresh_token)
  localStorage.setItem('weknora_lite_mode', 'true')
  localStorage.setItem(BIDREVIEW_ROLE_KEY, session.bidreview_role || 'member')
  applyDefaultKnowledgeBase(session.default_knowledge_base)
  applyDefaultAgent(session.default_agent)
}

function applyDefaultKnowledgeBase(kb?: BidReviewDefaultKnowledgeBase): void {
  const kbId = typeof kb?.id === 'string' ? kb.id.trim() : ''
  if (!kbId) return
  const knowledgeBase = {
    id: kbId,
    name: kb?.name || '投标业务知识库',
    description: kb?.description || '',
  }
  localStorage.setItem('weknora_current_kb', JSON.stringify(knowledgeBase))
  localStorage.setItem('weknora_knowledge_bases', JSON.stringify([knowledgeBase]))
  const rawSettings = localStorage.getItem('WeKnora_settings')
  let settings: Record<string, unknown> = {}
  if (rawSettings) {
    try {
      settings = JSON.parse(rawSettings)
    } catch {
      settings = {}
    }
  }
  settings.selectedKnowledgeBases = [kbId]
  settings.selectedFiles = []
  settings.selectedFileKbMap = {}
  localStorage.setItem('WeKnora_settings', JSON.stringify(settings))
}

function applyDefaultAgent(agent?: BidReviewDefaultAgent): void {
  const agentId = typeof agent?.id === 'string' ? agent.id.trim() : ''
  if (!agentId) return
  const rawSettings = localStorage.getItem('WeKnora_settings')
  let settings: Record<string, unknown> = {}
  if (rawSettings) {
    try {
      settings = JSON.parse(rawSettings)
    } catch {
      settings = {}
    }
  }
  settings.selectedAgentId = agentId
  settings.selectedAgentSourceTenantId = null
  settings.isAgentEnabled = true
  settings.selectedKnowledgeBases = []
  settings.selectedFiles = []
  settings.selectedFileKbMap = {}
  localStorage.setItem('WeKnora_settings', JSON.stringify(settings))
  localStorage.setItem('weknora_bidreview_default_agent', JSON.stringify({
    id: agentId,
    name: agent?.name || 'G博士',
    source: agent?.source || 'bidreview_default',
  }))
}
