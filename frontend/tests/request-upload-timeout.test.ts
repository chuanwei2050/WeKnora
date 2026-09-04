import { beforeEach, describe, expect, it, vi } from 'vitest'

const { postMock } = vi.hoisted(() => ({
  postMock: vi.fn(),
}))

vi.mock('axios', () => ({
  default: {
    create: () => ({
      interceptors: {
        request: { use: vi.fn() },
        response: { use: vi.fn() },
      },
      post: postMock,
      get: vi.fn(),
      put: vi.fn(),
      patch: vi.fn(),
      delete: vi.fn(),
    }),
  },
}))

import { postUpload } from '../src/utils/request'

describe('postUpload', () => {
  beforeEach(() => {
    postMock.mockReset()
    postMock.mockResolvedValue({ data: { success: true } })
  })

  it('does not inherit the 30 second timeout used by regular API requests', async () => {
    const formData = new FormData()

    await postUpload('/upload', formData)

    expect(postMock).toHaveBeenCalledWith('/upload', formData, expect.objectContaining({ timeout: 0 }))
  })
})
