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
    expect(initial.size).toEqual({ width: 360, height: 480 })
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
    expect(parseFrameMessage({ version: 1, type: 'open-document', knowledgeBaseId: 'kb-1', knowledgeId: 'doc-1' })).toEqual({
      version: 1,
      type: 'open-document',
      knowledgeBaseId: 'kb-1',
      knowledgeId: 'doc-1',
    })
    expect(parseFrameMessage({ version: 1, type: 'open-document', knowledgeBaseId: '../login' })).toBeNull()
  })

  it('rejects cross-origin frames and unsafe runtime configuration', () => {
    expect(() => initWidget({ ...config(), instanceId: 'cross-origin', iframeUrl: 'https://untrusted.example/widget' })).toThrow(/host origin/)
    expect(() => initWidget({ ...config(), instanceId: 'unsafe-color', theme: { primaryColor: 'red' } })).toThrow(/primaryColor/)
    expect(parseFrameMessage({ version: 1, type: 'error', code: 'x', message: 'x'.repeat(1025) })).toBeNull()
    expect(() => initWidget({ ...config(), instanceId: 'unsafe-kb', selection: { mode: 'fixed', knowledgeBaseIds: ['../foreign'] } })).toThrow(/knowledge base id/)
    expect(() => initWidget({ ...config(), instanceId: 'all-with-ids', selection: { mode: 'all-allowed', knowledgeBaseIds: ['kb-1'] } as never })).toThrow(/cannot include/)
  })

  it('allows an explicitly targeted independent widget origin', () => {
    const instance = initWidget({
      ...config(),
      instanceId: 'independent-origin',
      iframeUrl: 'https://knowledge.example/knowledge/embed/embed/widget',
      targetOrigin: 'https://knowledge.example',
      theme: { iconUrl: 'https://knowledge.example/widget/icons/ai-assistant.png' },
    })
    expect(instance).toBeDefined()
    instance.destroy()
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
    expect(host?.shadowRoot?.querySelector<HTMLImageElement>('.weknora-launcher img')?.src).toContain('/widget/icons/ai-assistant.png')
    expect(host?.shadowRoot?.querySelector('[aria-label^="调整聊天窗口大小"]')?.classList.contains('weknora-resize-handle')).toBe(true)
    instance.destroy()
  })

  it('changes to a docked launcher only when released near a viewport edge', () => {
    const instance = initWidget(config())
    const host = document.querySelector<HTMLElement>('[data-weknora-widget]')
    const launcher = host?.shadowRoot?.querySelector<HTMLButtonElement>('.weknora-launcher')
    expect(launcher).not.toBeNull()
    if (!launcher) return
    launcher.setPointerCapture = vi.fn()
    launcher.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true, button: 0, clientX: 900, clientY: 500 }))
    launcher.dispatchEvent(new MouseEvent('pointermove', { bubbles: true, clientX: -100, clientY: 500 }))
    launcher.dispatchEvent(new MouseEvent('pointerup', { bubbles: true, clientX: -100, clientY: 500 }))
    expect(launcher.style.left).toBe('0px')
    expect(launcher.style.width).toBe('46px')
    expect(launcher.dataset.docked).toBe('left')
    instance.destroy()
  })

  it('keeps the launcher free when released outside the docking distance', () => {
    const instance = initWidget(config())
    const host = document.querySelector<HTMLElement>('[data-weknora-widget]')
    const launcher = host?.shadowRoot?.querySelector<HTMLButtonElement>('.weknora-launcher')
    expect(launcher).not.toBeNull()
    if (!launcher) return
    launcher.setPointerCapture = vi.fn()
    launcher.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true, button: 0, clientX: 900, clientY: 500 }))
    launcher.dispatchEvent(new MouseEvent('pointermove', { bubbles: true, clientX: 500, clientY: 500 }))
    launcher.dispatchEvent(new MouseEvent('pointerup', { bubbles: true, clientX: 500, clientY: 500 }))
    expect(launcher.style.left).not.toBe('0px')
    expect(launcher.style.width).toBe('64px')
    expect(launcher.dataset.docked).toBeUndefined()
    instance.destroy()
  })

  it('does not dock the launcher on a click without dragging', () => {
    const instance = initWidget(config())
    const host = document.querySelector<HTMLElement>('[data-weknora-widget]')
    const launcher = host?.shadowRoot?.querySelector<HTMLButtonElement>('.weknora-launcher')
    expect(launcher).not.toBeNull()
    if (!launcher) return
    launcher.setPointerCapture = vi.fn()
    launcher.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true, button: 0, clientX: 944, clientY: 688 }))
    launcher.dispatchEvent(new MouseEvent('pointerup', { bubbles: true, clientX: 944, clientY: 688 }))
    expect(launcher.dataset.docked).toBeUndefined()
    expect(launcher.style.width).toBe('64px')
    instance.destroy()
  })

  it('buffers early authentication and replays ready to late listeners', async () => {
    const instance = initWidget(config())
    const host = document.querySelector<HTMLElement>('[data-weknora-widget]')
    const iframe = host?.shadowRoot?.querySelector<HTMLIFrameElement>('iframe')
    expect(iframe).not.toBeNull()
    expect(iframe?.getAttribute('sandbox')).toContain('allow-modals')
    expect(iframe?.getAttribute('sandbox')).toContain('allow-popups')
    expect(iframe?.getAttribute('sandbox')).toContain('allow-popups-to-escape-sandbox')
    expect(iframe?.allow).toContain('clipboard-write')

    expect(() => instance.authenticate('ticket')).not.toThrow()
    window.dispatchEvent(new MessageEvent('message', {
      origin: window.location.origin,
      source: iframe?.contentWindow ?? null,
      data: { version: 1, type: 'ready' },
    }))

    const lateReadyListener = vi.fn()
    instance.on('ready', lateReadyListener)
    await Promise.resolve()
    expect(lateReadyListener).toHaveBeenCalledOnce()
    instance.destroy()
  })
})
