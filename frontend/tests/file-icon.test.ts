import { describe, expect, it } from 'vitest'
import { getFileIcon } from '../src/utils/files'

describe('getFileIcon', () => {
  it('uses registered TDesign icons for text files', () => {
    expect(getFileIcon('notes.txt')).toBe('file-txt')
    expect(getFileIcon({ file_type: '.md' })).toBe('file-markdown')
  })

  it('keeps special knowledge item icons', () => {
    expect(getFileIcon({ type: 'manual' })).toBe('edit')
    expect(getFileIcon({ type: 'url' })).toBe('link')
  })
})
