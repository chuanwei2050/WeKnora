type BidReviewSession = {
  user?: unknown
  tenant?: unknown
  token?: string
  refresh_token?: string
  bidreview_role?: string
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
}
