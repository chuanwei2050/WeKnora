import type {
  NormalLayout,
  WidgetConfig,
  WidgetEventName,
  WidgetInstance,
  WidgetLayout,
  WidgetPosition,
  WidgetSize,
} from './types'
import { parseFrameMessage, parseStoredLayout, parseWidgetConfig } from './validation'

const instances = new Map<string, WidgetInstance>()
const MIN_SIZE: WidgetSize = { width: 320, height: 420 }
const DEFAULT_SIZE: WidgetSize = { width: 400, height: 620 }
const EDGE_GAP = 16

function viewportRect() {
  const viewport = window.visualViewport
  return {
    x: viewport?.offsetLeft ?? 0,
    y: viewport?.offsetTop ?? 0,
    width: viewport?.width ?? window.innerWidth,
    height: viewport?.height ?? window.innerHeight,
  }
}

function clampNormal(layout: NormalLayout): NormalLayout {
  const viewport = viewportRect()
  const gapX = Math.min(EDGE_GAP, Math.max(0, (viewport.width - 1) / 2))
  const gapY = Math.min(EDGE_GAP, Math.max(0, (viewport.height - 1) / 2))
  const availableWidth = Math.max(1, viewport.width - gapX * 2)
  const availableHeight = Math.max(1, viewport.height - gapY * 2)
  const width = Math.min(Math.max(layout.size.width, Math.min(MIN_SIZE.width, availableWidth)), availableWidth)
  const height = Math.min(Math.max(layout.size.height, Math.min(MIN_SIZE.height, availableHeight)), availableHeight)
  const minX = viewport.x + gapX
  const minY = viewport.y + gapY
  const maxX = Math.max(minX, viewport.x + viewport.width - width - gapX)
  const maxY = Math.max(minY, viewport.y + viewport.height - height - gapY)
  return {
    mode: 'normal',
    position: {
      x: Math.min(Math.max(layout.position.x, minX), maxX),
      y: Math.min(Math.max(layout.position.y, minY), maxY),
    },
    size: { width, height },
  }
}

function defaultLayout(config: WidgetConfig): NormalLayout {
  const viewport = viewportRect()
  const size = config.initialSize ?? DEFAULT_SIZE
  const position = config.initialPosition ?? {
    x: viewport.x + viewport.width - size.width - 24,
    y: viewport.y + viewport.height - size.height - 24,
  }
  return clampNormal({ mode: 'normal', position, size })
}

function applyStyles(element: HTMLElement, styles: Partial<CSSStyleDeclaration>) {
  Object.assign(element.style, styles)
}

export function initWidget(rawConfig: WidgetConfig): WidgetInstance {
  const config = parseWidgetConfig(rawConfig)
  const existing = instances.get(config.instanceId)
  if (existing) return existing

  const storageKey = `weknora-widget-layout:${config.instanceId}`
  const restored = parseStoredLayout(sessionStorage.getItem(storageKey))
  const initial = defaultLayout(config)
  let layout: WidgetLayout = restored ? clampNormal(restored) : initial
  let opened = false
  let destroyed = false
  const listeners = new Map<WidgetEventName, Set<(detail: unknown) => void>>()

  const host = document.createElement('div')
  host.dataset.weknoraWidget = config.instanceId
  applyStyles(host, { position: 'fixed', inset: '0', zIndex: '2147483000', pointerEvents: 'none' })
  const shadow = host.attachShadow({ mode: 'open' })

  const launcher = document.createElement('button')
  launcher.type = 'button'
  launcher.setAttribute('aria-label', config.theme?.title || '打开知识库聊天')
  if (config.theme?.iconUrl) {
    const icon = document.createElement('img')
    icon.src = new URL(config.theme.iconUrl, window.location.href).href
    icon.alt = ''
    applyStyles(icon, { width: '28px', height: '28px', objectFit: 'contain', pointerEvents: 'none' })
    launcher.append(icon)
  } else {
    launcher.textContent = '问'
  }
  applyStyles(launcher, {
    position: 'fixed', right: '24px', bottom: '24px', width: '56px', height: '56px', borderRadius: '50%',
    border: '0', color: '#fff', background: config.theme?.primaryColor || '#0052d9', cursor: 'grab',
    pointerEvents: 'auto', boxShadow: '0 8px 24px rgba(0,0,0,.22)', fontSize: '18px',
  })

  const panel = document.createElement('section')
  panel.setAttribute('role', 'dialog')
  panel.setAttribute('aria-label', config.theme?.title || '知识库聊天')
  applyStyles(panel, {
    position: 'fixed', display: 'none', overflow: 'hidden', borderRadius: '12px', background: config.theme?.colorMode === 'dark' ? '#1f1f1f' : '#fff',
    boxShadow: '0 16px 48px rgba(0,0,0,.28)', pointerEvents: 'auto',
  })

  const titlebar = document.createElement('header')
  titlebar.tabIndex = 0
  titlebar.setAttribute('aria-label', '拖动聊天窗口；方向键移动')
  titlebar.textContent = config.theme?.title || '知识库助手'
  applyStyles(titlebar, { height: '44px', padding: '0 88px 0 16px', display: 'flex', alignItems: 'center', cursor: 'move', userSelect: 'none', background: config.theme?.primaryColor || '#0052d9', color: '#fff' })

  const maximizeButton = document.createElement('button')
  maximizeButton.type = 'button'
  maximizeButton.setAttribute('aria-label', '最大化聊天窗口')
  maximizeButton.textContent = '□'
  applyStyles(maximizeButton, { position: 'absolute', top: '8px', right: '48px', width: '28px', height: '28px', pointerEvents: 'auto' })

  const closeButton = document.createElement('button')
  closeButton.type = 'button'
  closeButton.setAttribute('aria-label', '关闭聊天窗口')
  closeButton.textContent = '×'
  applyStyles(closeButton, { position: 'absolute', top: '8px', right: '12px', width: '28px', height: '28px', pointerEvents: 'auto' })

  const iframe = document.createElement('iframe')
  iframe.title = config.theme?.title || '知识库聊天内容'
  const iframeURL = new URL(config.iframeUrl)
  iframeURL.searchParams.set('instance_id', config.instanceId)
  iframeURL.searchParams.set('preserve_session', config.preserveSession === false ? 'false' : 'true')
  iframe.src = iframeURL.href
  iframe.referrerPolicy = 'strict-origin'
  iframe.setAttribute('sandbox', 'allow-scripts allow-same-origin allow-forms allow-downloads')
  applyStyles(iframe, { width: '100%', height: 'calc(100% - 44px)', border: '0', display: 'block' })

  const resizeHandle = document.createElement('div')
  resizeHandle.tabIndex = 0
  resizeHandle.setAttribute('role', 'separator')
  resizeHandle.setAttribute('aria-label', '调整聊天窗口大小；方向键缩放')
  applyStyles(resizeHandle, { position: 'absolute', right: '0', bottom: '0', width: '24px', height: '24px', cursor: 'nwse-resize' })

  panel.append(titlebar, maximizeButton, closeButton, iframe, resizeHandle)
  shadow.append(launcher, panel)
  document.body.append(host)

  const emit = (event: WidgetEventName, detail: unknown = undefined) => {
    listeners.get(event)?.forEach((listener) => listener(detail))
    host.dispatchEvent(new CustomEvent(`weknora:${event}`, { detail }))
  }

  const render = (notify = true) => {
    const viewport = viewportRect()
    const normal = layout.mode === 'normal' ? clampNormal(layout) : layout.restore
    if (layout.mode === 'normal') {
      layout = normal
      applyStyles(panel, { left: `${normal.position.x}px`, top: `${normal.position.y}px`, width: `${normal.size.width}px`, height: `${normal.size.height}px`, borderRadius: '12px' })
      maximizeButton.textContent = '□'
      maximizeButton.setAttribute('aria-label', '最大化聊天窗口')
      sessionStorage.setItem(storageKey, JSON.stringify(normal))
    } else {
      applyStyles(panel, { left: `${viewport.x}px`, top: `${viewport.y}px`, width: `${viewport.width}px`, height: `${viewport.height}px`, borderRadius: '0' })
      maximizeButton.textContent = '❐'
      maximizeButton.setAttribute('aria-label', '还原聊天窗口')
    }
    if (notify) emit('layout-changed', layout)
  }

  const moveBy = (dx: number, dy: number) => {
    if (layout.mode !== 'normal') return
    layout = clampNormal({ ...layout, position: { x: layout.position.x + dx, y: layout.position.y + dy } })
    render()
  }
  const resizeBy = (dw: number, dh: number) => {
    if (layout.mode !== 'normal') return
    layout = clampNormal({ ...layout, size: { width: layout.size.width + dw, height: layout.size.height + dh } })
    render()
  }

  const pointerDrag = (target: HTMLElement, operation: (dx: number, dy: number) => void) => {
    target.addEventListener('pointerdown', (event) => {
      if (event.button !== 0) return
      let x = event.clientX
      let y = event.clientY
      target.setPointerCapture(event.pointerId)
      const move = (next: PointerEvent) => {
        operation(next.clientX - x, next.clientY - y)
        x = next.clientX
        y = next.clientY
      }
      const end = () => {
        target.removeEventListener('pointermove', move)
        target.removeEventListener('pointerup', end)
        target.removeEventListener('pointercancel', end)
        target.removeEventListener('lostpointercapture', end)
      }
      target.addEventListener('pointermove', move)
      target.addEventListener('pointerup', end)
      target.addEventListener('pointercancel', end)
      target.addEventListener('lostpointercapture', end)
    })
  }
  pointerDrag(titlebar, moveBy)
  pointerDrag(resizeHandle, resizeBy)

  const initialViewport = viewportRect()
  let launcherPosition = { x: initialViewport.x + initialViewport.width - 80, y: initialViewport.y + initialViewport.height - 80 }
  let launcherMoved = false
  const clampLauncher = () => {
    const viewport = viewportRect()
    const gapX = Math.min(EDGE_GAP, Math.max(0, (viewport.width - 56) / 2))
    const gapY = Math.min(EDGE_GAP, Math.max(0, (viewport.height - 56) / 2))
    const maxX = Math.max(viewport.x + gapX, viewport.x + viewport.width - 56 - gapX)
    const maxY = Math.max(viewport.y + gapY, viewport.y + viewport.height - 56 - gapY)
    launcherPosition = {
      x: Math.min(Math.max(launcherPosition.x, viewport.x + gapX), maxX),
      y: Math.min(Math.max(launcherPosition.y, viewport.y + gapY), maxY),
    }
    applyStyles(launcher, { left: `${launcherPosition.x}px`, top: `${launcherPosition.y}px`, right: 'auto', bottom: 'auto' })
  }
  clampLauncher()
  pointerDrag(launcher, (dx, dy) => {
    if (dx === 0 && dy === 0) return
    launcherMoved = true
    launcherPosition = { x: launcherPosition.x + dx, y: launcherPosition.y + dy }
    clampLauncher()
  })

  const keyboardLayout = (operation: (dx: number, dy: number) => void) => (event: KeyboardEvent) => {
    const step = event.shiftKey ? 40 : 10
    const deltas: Record<string, [number, number]> = { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -step], ArrowDown: [0, step] }
    const delta = deltas[event.key]
    if (!delta) return
    event.preventDefault()
    operation(...delta)
  }
  titlebar.addEventListener('keydown', keyboardLayout(moveBy))
  resizeHandle.addEventListener('keydown', keyboardLayout(resizeBy))

  const instance: WidgetInstance = {
    authenticate(ticket) {
      if (destroyed || !ticket || ticket.length > 512) throw new Error('Invalid bootstrap ticket')
      iframe.contentWindow?.postMessage({ version: 1, type: 'auth-ready', ticket }, config.targetOrigin!)
    },
    open() { if (destroyed || opened) return; opened = true; panel.style.display = 'block'; launcher.style.display = 'none'; render(false); emit('open') },
    close() { if (destroyed || !opened) return; opened = false; panel.style.display = 'none'; launcher.style.display = 'block'; emit('close') },
    destroy() { if (destroyed) return; destroyed = true; window.removeEventListener('message', onMessage); window.removeEventListener('resize', onViewportChange); window.visualViewport?.removeEventListener('resize', onViewportChange); host.remove(); listeners.clear(); instances.delete(config.instanceId) },
    moveTo(position) { if (layout.mode !== 'normal') return; layout = clampNormal({ ...layout, position }); render() },
    resizeTo(size) { if (layout.mode !== 'normal') return; layout = clampNormal({ ...layout, size }); render() },
    maximize() { if (layout.mode === 'maximized') return; layout = { mode: 'maximized', restore: clampNormal(layout) }; render() },
    restore() { if (layout.mode !== 'maximized') return; layout = clampNormal(layout.restore); render() },
    resetLayout() { layout = initial; sessionStorage.removeItem(storageKey); render() },
    getLayout() { return structuredClone(layout) },
    on(event, listener) { const bucket = listeners.get(event) ?? new Set(); bucket.add(listener); listeners.set(event, bucket); return () => bucket.delete(listener) },
  }

  const onMessage = (event: MessageEvent) => {
    if (event.origin !== config.targetOrigin || event.source !== iframe.contentWindow) return
    const message = parseFrameMessage(event.data)
    if (!message) return
    if (message.type === 'ready') {
      iframe.contentWindow?.postMessage({ version: 1, type: 'configure', selection: config.selection, theme: config.theme }, config.targetOrigin!)
    }
    emit(message.type === 'answer-completed' ? 'answer-completed' : message.type, message)
  }
  const onViewportChange = () => {
    layout = layout.mode === 'normal'
      ? clampNormal(layout)
      : { mode: 'maximized', restore: clampNormal(layout.restore) }
    render()
    clampLauncher()
  }
  window.addEventListener('message', onMessage)
  window.addEventListener('resize', onViewportChange)
  window.visualViewport?.addEventListener('resize', onViewportChange)

  launcher.addEventListener('click', () => {
    if (launcherMoved) { launcherMoved = false; return }
    instance.open()
  })
  closeButton.addEventListener('click', () => instance.close())
  maximizeButton.addEventListener('click', () => layout.mode === 'normal' ? instance.maximize() : instance.restore())
  instances.set(config.instanceId, instance)
  render(false)
  return instance
}

export type * from './types'
