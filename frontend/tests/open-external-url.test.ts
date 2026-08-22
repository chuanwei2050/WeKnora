import { afterEach, describe, expect, it, vi } from 'vitest'
import { openExternalUrl } from '../src/utils/open-external-url'

afterEach(() => {
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

describe('openExternalUrl', () => {
  it('returns false for empty urls', () => {
    expect(openExternalUrl('')).toBe(false)
    expect(openExternalUrl('   ')).toBe(false)
  })

  it('prefers window.open when available', () => {
    const opened = { closed: false } as Window
    const openSpy = vi.spyOn(window, 'open').mockReturnValue(opened)
    expect(openExternalUrl('https://example.com/docs')).toBe(true)
    expect(openSpy).toHaveBeenCalledWith('https://example.com/docs', '_blank', 'noopener,noreferrer')
  })

  it('falls back to a blank-target anchor when window.open is blocked', () => {
    vi.spyOn(window, 'open').mockReturnValue(null)
    const click = vi.fn()
    const originalCreate = document.createElement.bind(document)
    vi.spyOn(document, 'createElement').mockImplementation((tagName: string) => {
      const el = originalCreate(tagName)
      if (tagName === 'a') {
        Object.defineProperty(el, 'click', { value: click })
        Object.defineProperty(el, 'target', { writable: true, value: '' })
        Object.defineProperty(el, 'download', { writable: true, value: '' })
      }
      return el
    })

    expect(openExternalUrl('https://example.com/file.csv', { downloadName: 'failed.csv' })).toBe(true)
    expect(click).toHaveBeenCalledOnce()
  })
})