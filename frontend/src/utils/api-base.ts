export function getApiBaseUrl(): string {
  const configured = String(import.meta.env.VITE_API_BASE_PATH || '').trim()
  if (configured) return normalizeBasePath(configured, 'VITE_API_BASE_PATH')
  // Same-origin embed under /knowledge/embed/ must use nginx's /knowledge/api/
  // proxy so host apps that also own /api/ (e.g. bidder-agent) do not collide.
  if (isKnowledgeEmbedPath()) return '/knowledge'
  // Standalone / cross-origin WeKnora origin: /api is served directly.
  return '';
}

export function getFileEndpoint(): string {
  const configured = String(import.meta.env.VITE_FILE_ENDPOINT || '').trim()
  if (configured) return normalizeBasePath(configured, 'VITE_FILE_ENDPOINT')
  if (isKnowledgeEmbedPath()) return '/knowledge/files'
  return '/files'
}

function isKnowledgeEmbedPath(): boolean {
  if (typeof window === 'undefined') return false
  return window.location.pathname.startsWith('/knowledge/embed/')
}

function normalizeBasePath(value: string, name: string): string {
  if (!value.startsWith('/') || value.includes('://') || value.includes('..')) {
    throw new Error(`${name} must be an absolute same-origin path`)
  }
  return value.length > 1 ? value.replace(/\/+$/, '') : ''
}
