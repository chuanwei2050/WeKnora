export function getApiBaseUrl(): string {
  const configured = String(import.meta.env.VITE_API_BASE_PATH || '').trim()
  if (configured) return normalizeBasePath(configured, 'VITE_API_BASE_PATH')
  const mode = new URLSearchParams(window.location.search).get('mode')
  if (mode === 'embedded-page' || mode === 'embedded-widget' || window.location.pathname.startsWith('/knowledge/embed/')) {
    return '/knowledge'
  }
  // Use same-origin requests by default.
  // In local Vite dev, `vite.config.ts` proxies `/api` to the Go backend.
  return '';
}

export function getFileEndpoint(): string {
  const configured = String(import.meta.env.VITE_FILE_ENDPOINT || '').trim()
  if (configured) return normalizeBasePath(configured, 'VITE_FILE_ENDPOINT')
  return getApiBaseUrl() === '/knowledge' ? '/knowledge/files' : '/files'
}

function normalizeBasePath(value: string, name: string): string {
  if (!value.startsWith('/') || value.includes('://') || value.includes('..')) {
    throw new Error(`${name} must be an absolute same-origin path`)
  }
  return value.length > 1 ? value.replace(/\/+$/, '') : ''
}
