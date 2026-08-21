import { afterEach, describe, expect, it } from 'vitest'
import { createIdempotencyKey } from '../src/utils/idempotency-key'

const uuidV4Pattern = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i
const originalCrypto = globalThis.crypto

afterEach(() => {
  Object.defineProperty(globalThis, 'crypto', { configurable: true, value: originalCrypto })
})

describe('createIdempotencyKey', () => {
  it('still produces RFC 4122 v4 UUIDs when crypto.randomUUID is unavailable', () => {
    Object.defineProperty(globalThis, 'crypto', { configurable: true, value: undefined })
    expect(createIdempotencyKey()).toMatch(uuidV4Pattern)
  })

  it('uses getRandomValues when randomUUID is missing, as on HTTP LAN origins', () => {
    const deterministicBytes = Uint8Array.from({ length: 16 }, (_, index) => index)
    Object.defineProperty(globalThis, 'crypto', {
      configurable: true,
      value: {
        getRandomValues: (bytes: Uint8Array) => {
          bytes.set(deterministicBytes)
          return bytes
        },
      },
    })
    expect(createIdempotencyKey()).toBe('00010203-0405-4607-8809-0a0b0c0d0e0f')
  })

  it('prefers the platform randomUUID when the function exists', () => {
    Object.defineProperty(globalThis, 'crypto', {
      configurable: true,
      value: { randomUUID: () => 'provider-uuid' },
    })
    expect(createIdempotencyKey()).toBe('provider-uuid')
  })
})
