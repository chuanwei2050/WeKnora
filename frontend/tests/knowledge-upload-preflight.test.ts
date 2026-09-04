import { beforeEach, describe, expect, it, vi } from 'vitest'

const { getMock, postUploadMock, calculateFileMD5Mock } = vi.hoisted(() => ({
  getMock: vi.fn(),
  postUploadMock: vi.fn(),
  calculateFileMD5Mock: vi.fn(),
}))

vi.mock('../src/utils/request', () => ({
  get: getMock,
  post: vi.fn(),
  put: vi.fn(),
  del: vi.fn(),
  postUpload: postUploadMock,
  getDown: vi.fn(),
}))

vi.mock('../src/utils/file-hash', () => ({
  calculateFileMD5: calculateFileMD5Mock,
}))

import { uploadKnowledgeFile } from '../src/api/knowledge-base'

describe('uploadKnowledgeFile preflight', () => {
  beforeEach(() => {
    getMock.mockReset()
    postUploadMock.mockReset()
    calculateFileMD5Mock.mockReset()
    calculateFileMD5Mock.mockResolvedValue('5eb63bbbe01eeed093cb22bb8f5acdc3')
  })

  it('returns the existing document without uploading duplicate bytes', async () => {
    const existing = { id: 'existing' }
    getMock.mockResolvedValue({ success: true, data: { exists: true, knowledge: existing } })

    await expect(uploadKnowledgeFile('kb', { file: new File(['hello world'], 'report.docx') }))
      .rejects.toMatchObject({ code: 'duplicate_file', data: existing })
    expect(postUploadMock).not.toHaveBeenCalled()
  })

  it('uploads normally when no duplicate exists', async () => {
    getMock.mockResolvedValue({ success: true, data: { exists: false } })
    postUploadMock.mockResolvedValue({ success: true })

    await expect(uploadKnowledgeFile('kb', { file: new File(['new'], 'report.docx') }))
      .resolves.toEqual({ success: true })
    expect(postUploadMock).toHaveBeenCalledOnce()
  })

  it('falls back to uploading when preflight is unavailable', async () => {
    getMock.mockRejectedValue(new Error('not deployed'))
    postUploadMock.mockResolvedValue({ success: true })

    await expect(uploadKnowledgeFile('kb', { file: new File(['new'], 'report.docx') }))
      .resolves.toEqual({ success: true })
    expect(postUploadMock).toHaveBeenCalledOnce()
  })
})
