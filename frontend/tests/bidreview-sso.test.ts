import { afterEach, describe, expect, it } from 'vitest'
import { canManageBidReviewKnowledge } from '../src/utils/bidreview-sso'
import { setEmbeddedScopes } from '../src/utils/embedded-runtime'

afterEach(() => {
  window.history.replaceState({}, '', '/')
  localStorage.clear()
  setEmbeddedScopes([])
})

describe('bid review knowledge permissions', () => {
  it('defers embedded-page authorization to the exchanged integration user role', () => {
    window.history.replaceState({}, '', '/knowledge/embed/platform/knowledge-bases?mode=embedded-page')
    setEmbeddedScopes(['knowledge:write'])
    expect(canManageBidReviewKnowledge()).toBe(true)
  })

  it('hides embedded management controls without knowledge write scope', () => {
    window.history.replaceState({}, '', '/knowledge/embed/platform/knowledge-bases?mode=embedded-page')
    setEmbeddedScopes(['knowledge:read'])
    expect(canManageBidReviewKnowledge()).toBe(false)
  })

  it('keeps the legacy embedded route role gate', () => {
    window.history.replaceState({}, '', '/knowledge/platform/knowledge-bases')
    expect(canManageBidReviewKnowledge()).toBe(false)
    localStorage.setItem('weknora_bidreview_role', 'tenant_admin')
    expect(canManageBidReviewKnowledge()).toBe(true)
  })
})
