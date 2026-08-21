export type RuntimeMode = 'standalone' | 'embedded-page' | 'embedded-widget'

const ALLOWED_MODES = new Set<RuntimeMode>(['standalone', 'embedded-page', 'embedded-widget'])
let csrfToken = ''
let sessionToken = ''
let embeddedScopes = new Set<string>()

export function getRuntimeMode(location: Pick<Location, 'pathname' | 'search'> = window.location): RuntimeMode {
  const requested = new URLSearchParams(location.search).get('mode') as RuntimeMode | null
  if (requested && ALLOWED_MODES.has(requested)) return requested
  if (location.pathname.includes('/embed/widget')) return 'embedded-widget'
  if (location.pathname.startsWith('/knowledge/embed/')) return 'embedded-page'
  return 'standalone'
}

export function getEmbeddedParentOrigin(location: Pick<Location, 'search'> = window.location): string {
  const configured = new URLSearchParams(location.search).get('parent_origin')
  return resolveEmbeddedParentOrigin(configured, document.referrer, window.location.origin)
}

export function isCookieEmbeddedMode(): boolean {
  return getRuntimeMode() === 'embedded-page' || getRuntimeMode() === 'embedded-widget'
}

export function setEmbeddedCSRFToken(value: string): void {
  csrfToken = value
}

export function getEmbeddedCSRFToken(): string {
  return csrfToken
}

export function setEmbeddedSessionToken(value: string): void {
  sessionToken = value
}

export function getEmbeddedSessionToken(): string {
  return sessionToken
}

export function getEmbeddedAuthHeaders(options: { csrf?: boolean; json?: boolean } = {}): Record<string, string> {
  const headers: Record<string, string> = {}
  if (sessionToken) headers.Authorization = `Bearer ${sessionToken}`
  if (options.csrf) {
    const csrf = getEmbeddedCSRFToken()
    if (csrf) headers['X-CSRF-Token'] = csrf
  }
  if (options.json) headers['Content-Type'] = 'application/json'
  return headers
}

export function setEmbeddedScopes(scopes: string[]): void {
  embeddedScopes = new Set(scopes)
}

export function hasEmbeddedScope(scope: string): boolean {
  return embeddedScopes.has(scope)
}

export function resolveEmbeddedParentOrigin(configured: string | null, referrer: string, currentOrigin: string): string {
  for (const candidate of [configured, referrer]) {
    if (!candidate) continue
    try {
      const url = new URL(candidate)
      if (url.protocol === 'http:' || url.protocol === 'https:') return url.origin
    } catch {
      // Ignore malformed external values and keep the iframe on its own origin.
    }
  }
  return currentOrigin
}

export type EmbeddedInboundMessage =
  | { version: 1; type: 'auth-ready'; ticket: string }
  | { version: 1; type: 'new-conversation' }
  | { version: 1; type: 'toggle-conversations' }
  | { version: 1; type: 'set-theme'; theme: 'light' | 'dark' }
  | { version: 1; type: 'set-locale'; locale: string }
  | { version: 1; type: 'open-knowledge-base'; knowledgeBaseId: string }
  | { version: 1; type: 'configure'; selection: { mode: 'fixed' | 'selectable' | 'all-allowed'; knowledgeBaseIds?: string[]; initialKnowledgeBaseIds?: string[] }; theme?: { primaryColor?: string; title?: string; colorMode?: 'light' | 'dark' } }

export function parseEmbeddedMessage(value: unknown): EmbeddedInboundMessage | null {
  if (!value || typeof value !== 'object') return null
  const message = value as Record<string, unknown>
  if (message.version !== 1 || typeof message.type !== 'string') return null
  if (message.type === 'auth-ready' && typeof message.ticket === 'string' && message.ticket.length > 0 && message.ticket.length <= 512) {
    return { version: 1, type: 'auth-ready', ticket: message.ticket }
  }
  if (message.type === 'new-conversation') return { version: 1, type: 'new-conversation' }
  if (message.type === 'toggle-conversations') return { version: 1, type: 'toggle-conversations' }
  if (message.type === 'set-theme' && (message.theme === 'light' || message.theme === 'dark')) {
    return { version: 1, type: 'set-theme', theme: message.theme }
  }
  if (message.type === 'set-locale' && typeof message.locale === 'string' && /^[a-z]{2}(?:-[A-Z]{2})?$/.test(message.locale)) {
    return { version: 1, type: 'set-locale', locale: message.locale }
  }
  if (message.type === 'open-knowledge-base' && typeof message.knowledgeBaseId === 'string' && /^[a-zA-Z0-9_-]{1,128}$/.test(message.knowledgeBaseId)) {
    return { version: 1, type: 'open-knowledge-base', knowledgeBaseId: message.knowledgeBaseId }
  }
  if (message.type === 'configure' && message.selection && typeof message.selection === 'object') {
    const selection = message.selection as Record<string, unknown>
    if (selection.mode !== 'fixed' && selection.mode !== 'selectable' && selection.mode !== 'all-allowed') return null
    const ids = selection.mode === 'fixed' ? selection.knowledgeBaseIds : selection.initialKnowledgeBaseIds
    if (ids !== undefined && (!Array.isArray(ids) || ids.length > 100 || !ids.every((id) => typeof id === 'string' && /^[a-zA-Z0-9_-]{1,128}$/.test(id)))) return null
    if (selection.mode === 'fixed' && (!Array.isArray(ids) || ids.length === 0)) return null
    if (selection.mode === 'all-allowed' && (selection.knowledgeBaseIds !== undefined || selection.initialKnowledgeBaseIds !== undefined)) return null
    const parsedIds = Array.isArray(ids) ? ids.filter((id): id is string => typeof id === 'string') : undefined
    const rawTheme = message.theme && typeof message.theme === 'object' ? message.theme as Record<string, unknown> : undefined
    const colorMode: 'light' | 'dark' | undefined = rawTheme?.colorMode === 'light'
      ? 'light'
      : rawTheme?.colorMode === 'dark'
        ? 'dark'
        : undefined
    if (rawTheme?.primaryColor !== undefined && (typeof rawTheme.primaryColor !== 'string' || !/^#[0-9a-f]{6}$/i.test(rawTheme.primaryColor))) return null
    if (rawTheme?.title !== undefined && (typeof rawTheme.title !== 'string' || rawTheme.title.length > 80)) return null
    const theme = rawTheme ? {
      primaryColor: typeof rawTheme.primaryColor === 'string' ? rawTheme.primaryColor : undefined,
      title: typeof rawTheme.title === 'string' ? rawTheme.title : undefined,
      colorMode,
    } : undefined
    if (selection.mode === 'fixed') return { version: 1, type: 'configure', selection: { mode: 'fixed', knowledgeBaseIds: parsedIds }, theme }
    if (selection.mode === 'selectable') return { version: 1, type: 'configure', selection: { mode: 'selectable', initialKnowledgeBaseIds: parsedIds }, theme }
    return { version: 1, type: 'configure', selection: { mode: 'all-allowed' }, theme }
  }
  return null
}

export function isIntegrationAuthFailure(error: unknown): boolean {
  const message = error instanceof Error ? error.message : String(error ?? '')
  if (/\b(401|403)\b/.test(message)) return true
  return /unauthorized|forbidden|csrf|pleaseRelogin/i.test(message)
}

export function notifyEmbeddedHost(type: 'ready' | 'unauthorized' | 'answer-completed' | 'route-change' | 'document-published' | 'open-document', data: Record<string, unknown> = {}): void {
  if (window.parent === window || getRuntimeMode() === 'standalone') return
  window.parent.postMessage({ version: 1, type, ...data }, getEmbeddedParentOrigin())
}
