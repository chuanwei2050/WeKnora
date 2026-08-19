import { describe, expect, it } from 'vitest'
import { isFileDrag } from '../src/utils/file-drag'

describe('isFileDrag', () => {
  it('accepts external file drags', () => {
    expect(isFileDrag({ types: ['Files'] } as unknown as DataTransfer)).toBe(true)
  })

  it('rejects internal folder reorder drags', () => {
    expect(isFileDrag({ types: ['text/plain'] } as unknown as DataTransfer)).toBe(false)
  })

  it('rejects missing drag data', () => {
    expect(isFileDrag(null)).toBe(false)
  })
})
