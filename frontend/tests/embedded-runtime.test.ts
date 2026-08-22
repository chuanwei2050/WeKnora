import { beforeEach, describe, expect, it } from 'vitest'
import {
  clearEmbeddedAuth,
  getEmbeddedAuthHeaders,
  getRuntimeMode,
  parseEmbeddedMessage,
  resolveEmbeddedParentOrigin,
  restoreEmbeddedAuth,
  setEmbeddedCSRFToken,
  setEmbeddedScopes,
  setEmbeddedSessionToken,
} from '../src/utils/embedded-runtime'

function installSessionStorage() {
  const store = new Map<string, string>()
  Object.defineProperty(globalThis, 'sessionStorage', {
    configurable: true,
    value: {
      getItem: (key: string) => store.get(key) ?? null,
      setItem: (key: string, value: string) => { store.set(key, value) },
      removeItem: (key: string) => { store.delete(key) },
      clear: () => { store.clear() },
    },
  })
}

describe('embedded runtime', () => {
  beforeEach(() => {
    installSessionStorage()
    clearEmbeddedAuth()
  })

  it('defaults to standalone and accepts explicit modes', () => {
    expect(getRuntimeMode({ pathname: '/', search: '' } as Location)).toBe('standalone')
    expect(getRuntimeMode({ pathname: '/', search: '?mode=embedded-page' } as Location)).toBe('embedded-page')
    expect(getRuntimeMode({ pathname: '/embed/widget', search: '' } as Location)).toBe('embedded-widget')
    expect(getRuntimeMode({ pathname: '/knowledge/embed/platform/knowledge-bases', search: '' } as Location)).toBe('embedded-page')
  })

  it('exposes bearer session headers for cross-site embeds', () => {
    setEmbeddedSessionToken('session-token')
    setEmbeddedCSRFToken('csrf-token')
    expect(getEmbeddedAuthHeaders({ csrf: true, json: true })).toEqual({
      Authorization: 'Bearer session-token',
      'X-CSRF-Token': 'csrf-token',
      'Content-Type': 'application/json',
    })
  })

  it('persists and restores embedded auth across reloads', () => {
    setEmbeddedSessionToken('session-token')
    setEmbeddedCSRFToken('csrf-token')
    setEmbeddedScopes(['knowledge:read'])
    const raw = sessionStorage.getItem('weknora_integration_embed_auth')
    expect(raw).toBeTruthy()
    // simulate reload: wipe memory, keep sessionStorage
    clearEmbeddedAuth()
    sessionStorage.setItem('weknora_integration_embed_auth', raw!)
    expect(restoreEmbeddedAuth()).toBe(true)
    expect(getEmbeddedAuthHeaders({ csrf: true })).toEqual({
      Authorization: 'Bearer session-token',
      'X-CSRF-Token': 'csrf-token',
    })
  })

  it('parses only versioned known messages', () => {
    expect(parseEmbeddedMessage({ version: 1, type: 'auth-ready', ticket: 'ticket' })).toEqual({ version: 1, type: 'auth-ready', ticket: 'ticket' })
    expect(parseEmbeddedMessage({ version: 1, type: 'new-conversation' })).toEqual({ version: 1, type: 'new-conversation' })
    expect(parseEmbeddedMessage({ version: 1, type: 'toggle-conversations' })).toEqual({ version: 1, type: 'toggle-conversations' })
    expect(parseEmbeddedMessage({ version: 1, type: 'set-theme', theme: 'unsafe' })).toBeNull()
    expect(parseEmbeddedMessage({ version: 2, type: 'auth-ready', ticket: 'ticket' })).toBeNull()
    expect(parseEmbeddedMessage({ version: 1, type: 'auth-ready', ticket: '' })).toBeNull()
    expect(parseEmbeddedMessage({ version: 1, type: 'configure', selection: { mode: 'fixed', knowledgeBaseIds: ['kb'] }, theme: { primaryColor: 'red' } })).toBeNull()
    expect(parseEmbeddedMessage({ version: 1, type: 'configure', selection: { mode: 'all-allowed', knowledgeBaseIds: ['kb'] } })).toBeNull()
  })

  it('falls back safely when an external parent origin is malformed', () => {
    expect(resolveEmbeddedParentOrigin('https://host.example/path', '', 'https://iframe.example')).toBe('https://host.example')
    expect(resolveEmbeddedParentOrigin('://bad', 'https://referrer.example/page', 'https://iframe.example')).toBe('https://referrer.example')
    expect(resolveEmbeddedParentOrigin('data:text/plain,opaque', 'https://referrer.example/page', 'https://iframe.example')).toBe('https://referrer.example')
    expect(resolveEmbeddedParentOrigin('://bad', 'also bad', 'https://iframe.example')).toBe('https://iframe.example')
  })
})
