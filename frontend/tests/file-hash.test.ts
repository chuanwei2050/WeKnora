import { describe, expect, it } from 'vitest'
import { calculateFileMD5 } from '../src/utils/file-hash'

describe('calculateFileMD5', () => {
  it('matches the MD5 format used by server-side duplicate detection', async () => {
    const file = new Blob(['hello world'])

    await expect(calculateFileMD5(file)).resolves.toBe('5eb63bbbe01eeed093cb22bb8f5acdc3')
  })
})
