import { describe, expect, it } from 'vitest'
import { getRuntimeMode, parseEmbeddedMessage } from '../src/utils/embedded-runtime'

describe('embedded runtime', () => {
  it('defaults to standalone and accepts explicit modes', () => {
    expect(getRuntimeMode({ pathname: '/', search: '' } as Location)).toBe('standalone')
    expect(getRuntimeMode({ pathname: '/', search: '?mode=embedded-page' } as Location)).toBe('embedded-page')
    expect(getRuntimeMode({ pathname: '/embed/widget', search: '' } as Location)).toBe('embedded-widget')
    expect(getRuntimeMode({ pathname: '/knowledge/embed/platform/knowledge-bases', search: '' } as Location)).toBe('embedded-page')
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
})
