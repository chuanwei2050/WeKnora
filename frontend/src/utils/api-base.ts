export function getApiBaseUrl(): string {
  const configured = String(import.meta.env.VITE_API_BASE_PATH || '').trim()
  if (configured) return normalizeBasePath(configured, 'VITE_API_BASE_PATH')
  // Embedded pages and widgets call the independent WeKnora API on their own
  // origin. In local Vite dev, `/api` is proxied to the Go backend.
  return '';
}

export function getFileEndpoint(): string {
  const configured = String(import.meta.env.VITE_FILE_ENDPOINT || '').trim()
  if (configured) return normalizeBasePath(configured, 'VITE_FILE_ENDPOINT')
  return '/files'
}

function normalizeBasePath(value: string, name: string): string {
  if (!value.startsWith('/') || value.includes('://') || value.includes('..')) {
    throw new Error(`${name} must be an absolute same-origin path`)
  }
  return value.length > 1 ? value.replace(/\/+$/, '') : ''
}
