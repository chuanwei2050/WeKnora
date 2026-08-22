/**
 * Open an external URL from standalone or sandboxed iframe contexts.
 *
 * Sandboxed iframes without `allow-popups` block `window.open` and `target=_blank`.
 * Prefer a new tab; fall back to a programmatic anchor with `target=_blank`.
 * Never navigate the current frame — that would unload an embedded knowledge app.
 */
export function openExternalUrl(
  url: string,
  options: { downloadName?: string } = {},
): boolean {
  const trimmed = String(url || '').trim()
  if (!trimmed) return false

  const runtime = (window as Window & { runtime?: { BrowserOpenURL?: (target: string) => void } }).runtime
  if (runtime?.BrowserOpenURL) {
    try {
      runtime.BrowserOpenURL(trimmed)
      return true
    } catch {
      // Fall through to browser APIs.
    }
  }

  try {
    const opened = window.open(trimmed, '_blank', 'noopener,noreferrer')
    if (opened) return true
  } catch {
    // Sandbox may throw or return null.
  }

  const anchor = document.createElement('a')
  anchor.href = trimmed
  anchor.rel = 'noopener noreferrer'
  anchor.target = '_blank'
  if (options.downloadName) {
    anchor.download = options.downloadName
  }
  anchor.style.display = 'none'
  document.body.appendChild(anchor)
  try {
    anchor.click()
    return true
  } finally {
    anchor.remove()
  }
}
