import { describe, expect, it } from 'vitest'
import { buildKnowledgeUploadMetadata } from '../src/utils/knowledge-upload-metadata'

describe('buildKnowledgeUploadMetadata', () => {
  it('omits metadata when governance is disabled', () => {
    expect(buildKnowledgeUploadMetadata('report.docx', false, 'managed_upload')).toBeUndefined()
  })

  it('builds complete metadata for managed drag-and-drop uploads', () => {
    const metadata = buildKnowledgeUploadMetadata(
      'report.docx',
      true,
      'managed_upload',
      new Date('2026-08-19T15:00:00.000Z'),
    )

    expect(JSON.parse(metadata || '')).toEqual({
      layer: 'experience',
      source_category: 'managed_upload',
      version_label: 'report.docx-2026-08-19T15:00:00.000Z',
      authority_level: 'internal',
    })
  })
})
