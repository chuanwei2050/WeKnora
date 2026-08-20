import { afterEach, describe, expect, it, vi } from 'vitest'
import { initWidget } from '../src/widget'
import { parseFrameMessage, parseStoredLayout } from '../src/widget/validation'

afterEach(() => {
  document.body.innerHTML = ''
  sessionStorage.clear()
})

const config = () => ({
  version: 1 as const,
  instanceId: `test-${Math.random().toString(16).slice(2)}`,
  iframeUrl: '/knowledge/embed/embed/widget',
  selection: { mode: 'fixed' as const, knowledgeBaseIds: ['kb-1'] as [string, ...string[]] },
})

describe('floating widget', () => {
  it('reuses an instance id and cleans up on destroy', () => {
    const value = config()
    const first = initWidget(value)
    expect(initWidget(value)).toBe(first)
    expect(document.querySelectorAll('[data-weknora-widget]').length).toBe(1)
    first.destroy()
    expect(document.querySelectorAll('[data-weknora-widget]').length).toBe(0)
  })

  it('moves, resizes, maximizes, restores and resets a bounded layout', () => {
    const instance = initWidget({ ...config(), initialPosition: { x: -500, y: -500 }, initialSize: { width: 10, height: 10 } })
    const initial = instance.getLayout()
    expect(initial.mode).toBe('normal')
    if (initial.mode !== 'normal') return
    expect(initial.position.x).toBeGreaterThanOrEqual(16)
    expect(initial.size).toEqual({ width: 320, height: 420 })
    instance.moveTo({ x: 100, y: 120 })
    instance.resizeTo({ width: 500, height: 550 })
    instance.maximize()
    expect(instance.getLayout().mode).toBe('maximized')
    instance.restore()
    expect(instance.getLayout()).toMatchObject({ mode: 'normal', position: { x: 100, y: 120 }, size: { width: 500, height: 550 } })
    instance.resetLayout()
    expect(instance.getLayout()).toEqual(initial)
    instance.destroy()
  })

  it('emits lifecycle events', () => {
    const instance = initWidget(config())
    const listener = vi.fn()
    instance.on('open', listener)
    instance.open()
    instance.open()
    expect(listener).toHaveBeenCalledTimes(1)
    expect(() => instance.authenticate('ticket')).not.toThrow()
    instance.destroy()
  })

  it('rejects malformed stored layouts and frame messages', () => {
    expect(parseStoredLayout('{"mode":"normal","position":{"x":"bad"}}')).toBeNull()
    expect(parseFrameMessage({ version: 1, type: 'answer-completed' })).toBeNull()
    expect(parseFrameMessage({ version: 2, type: 'ready' })).toBeNull()
    expect(parseFrameMessage({ version: 1, type: 'ready' })).toEqual({ version: 1, type: 'ready' })
  })

  it('rejects cross-origin frames and unsafe runtime configuration', () => {
    expect(() => initWidget({ ...config(), instanceId: 'cross-origin', iframeUrl: 'https://untrusted.example/widget' })).toThrow(/host origin/)
    expect(() => initWidget({ ...config(), instanceId: 'unsafe-color', theme: { primaryColor: 'red' } })).toThrow(/primaryColor/)
    expect(parseFrameMessage({ version: 1, type: 'error', code: 'x', message: 'x'.repeat(1025) })).toBeNull()
    expect(() => initWidget({ ...config(), instanceId: 'unsafe-kb', selection: { mode: 'fixed', knowledgeBaseIds: ['../foreign'] } })).toThrow(/knowledge base id/)
    expect(() => initWidget({ ...config(), instanceId: 'all-with-ids', selection: { mode: 'all-allowed', knowledgeBaseIds: ['kb-1'] } as never })).toThrow(/cannot include/)
  })

  it('supports multiple isolated instances and custom launcher icons', () => {
    const first = initWidget({ ...config(), instanceId: 'first', theme: { iconUrl: '/icon.svg' } })
    const second = initWidget({ ...config(), instanceId: 'second' })
    expect(document.querySelectorAll('[data-weknora-widget]').length).toBe(2)
    const firstHost = document.querySelector<HTMLElement>('[data-weknora-widget="first"]')
    expect(firstHost?.shadowRoot?.querySelector('img')?.getAttribute('src')).toContain('/icon.svg')
    first.destroy()
    second.destroy()
  })

  it('renders controls for creating and switching conversations', () => {
    const instance = initWidget(config())
    const host = document.querySelector<HTMLElement>('[data-weknora-widget]')
    expect(host?.shadowRoot?.querySelector('[aria-label="新建会话"]')).not.toBeNull()
    expect(host?.shadowRoot?.querySelector('[aria-label="切换对话"]')).not.toBeNull()
    instance.destroy()
  })
})
