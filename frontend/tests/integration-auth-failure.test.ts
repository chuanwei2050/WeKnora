import { describe, expect, it } from 'vitest'
import { isIntegrationAuthFailure } from '../src/utils/embedded-runtime'

describe('isIntegrationAuthFailure', () => {
  it('treats ticket and session 401/403 as authorization failures', () => {
    expect(isIntegrationAuthFailure(new Error('ticket exchange failed: 401'))).toBe(true)
    expect(isIntegrationAuthFailure(new Error('chat session creation failed: 403'))).toBe(true)
    expect(isIntegrationAuthFailure(new Error('session refresh failed: 401'))).toBe(true)
  })

  it('does not treat client runtime or rate-limit errors as lost authorization', () => {
    expect(isIntegrationAuthFailure(new TypeError('crypto.randomUUID is not a function'))).toBe(false)
    expect(isIntegrationAuthFailure(new Error('chat session creation failed: 429'))).toBe(false)
    expect(isIntegrationAuthFailure(new Error('knowledge base list failed: 500'))).toBe(false)
  })
})
