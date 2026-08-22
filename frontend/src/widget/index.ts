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
const MIN_SIZE: WidgetSize = { width: 360, height: 480 }
const DEFAULT_SIZE: WidgetSize = { width: 460, height: 700 }
const LAUNCHER_SIZE = 64
const DOCKED_LAUNCHER_WIDTH = 46
const DOCK_DISTANCE = 72
const EDGE_GAP = 16
const DEFAULT_ICON_URL = '/widget/icons/ai-assistant.png'

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
  let frameReady = false
  let pendingTicket: string | null = null
  const listeners = new Map<WidgetEventName, Set<(detail: unknown) => void>>()

  const host = document.createElement('div')
  host.dataset.weknoraWidget = config.instanceId
  applyStyles(host, { position: 'fixed', inset: '0', zIndex: '2147483000', pointerEvents: 'none' })
  const shadow = host.attachShadow({ mode: 'open' })

  const shellStyles = document.createElement('style')
  shellStyles.textContent = `
    @keyframes weknora-assistant-float { 0%, 100% { transform: translateY(0) } 50% { transform: translateY(-3px) } }
    @keyframes weknora-assistant-ring { 0%, 100% { opacity: .24; transform: scale(.9) } 50% { opacity: .06; transform: scale(1.1) } }
    .weknora-launcher:not([data-docked])::before { content: ''; position: absolute; inset: 4px; z-index: -1; border-radius: 22px; background: rgba(32,184,216,.22); filter: blur(8px); animation: weknora-assistant-ring 2.8s ease-in-out infinite; }
    .weknora-launcher[data-docked] { background: linear-gradient(160deg,#e9faff,#c8eff9) !important; box-shadow: 0 8px 24px rgba(16,91,125,.24) !important; }
    .weknora-launcher[data-docked="left"] { border-radius: 0 18px 18px 0 !important; }
    .weknora-launcher[data-docked="right"] { border-radius: 18px 0 0 18px !important; }
    .weknora-launcher[data-docked] img { width: 42px !important; height: 42px !important; animation: none !important; }
    .weknora-launcher:hover img { transform: translateY(-2px) scale(1.06); }
    .weknora-shell-button:hover { color: #0b5f8a !important; background: #e5f2f7 !important; }
    .weknora-shell-button:focus-visible, .weknora-launcher:focus-visible { outline: 3px solid rgba(32,184,216,.42); outline-offset: 2px; }
    .weknora-resize-handle::after { content: ''; position: absolute; right: 6px; bottom: 6px; width: 11px; height: 11px; border-right: 2px solid rgba(11,95,138,.7); border-bottom: 2px solid rgba(11,95,138,.7); border-radius: 0 0 3px; }
    @media (prefers-reduced-motion: reduce) { .weknora-launcher::before, .weknora-launcher img { animation: none !important; transition: none !important; } }
  `

  const launcher = document.createElement('button')
  launcher.type = 'button'
  launcher.className = 'weknora-launcher'
  launcher.setAttribute('aria-label', config.theme?.title || '打开知识库聊天')
  const icon = document.createElement('img')
  icon.src = new URL(config.theme?.iconUrl || DEFAULT_ICON_URL, window.location.href).href
  icon.alt = ''
  applyStyles(icon, { width: '56px', height: '56px', objectFit: 'contain', pointerEvents: 'none', transition: 'transform .22s ease', animation: 'weknora-assistant-float 3.6s ease-in-out infinite' })
  launcher.append(icon)
  applyStyles(launcher, {
    position: 'fixed', right: '24px', bottom: '24px', width: `${LAUNCHER_SIZE}px`, height: `${LAUNCHER_SIZE}px`, padding: '0', borderRadius: '22px',
    border: '0', color: config.theme?.primaryColor || '#0b5f8a', background: 'transparent', cursor: 'grab',
    pointerEvents: 'auto', boxShadow: 'none', fontSize: '14px', fontWeight: '700', overflow: 'visible', transition: 'left .22s ease, top .22s ease, width .22s ease, border-radius .22s ease, background .22s ease, box-shadow .2s ease',
  })

  const panel = document.createElement('section')
  panel.setAttribute('role', 'dialog')
  panel.setAttribute('aria-label', config.theme?.title || '知识库聊天')
  applyStyles(panel, {
    position: 'fixed', display: 'none', overflow: 'hidden', minWidth: '0', minHeight: '0', containerType: 'inline-size', borderRadius: '20px', background: config.theme?.colorMode === 'dark' ? '#1f1f1f' : '#fff',
    border: '1px solid rgba(28,53,68,.14)', boxShadow: '0 24px 72px rgba(15,45,61,.25)', pointerEvents: 'auto',
  })

  const titlebar = document.createElement('header')
  titlebar.tabIndex = 0
  titlebar.setAttribute('aria-label', '拖动聊天窗口；方向键移动')
  applyStyles(titlebar, { height: '64px', padding: '0 160px 0 14px', display: 'flex', alignItems: 'center', gap: '10px', cursor: 'move', userSelect: 'none', background: 'linear-gradient(110deg,#fff 0%,#f5fbfd 100%)', color: '#172b36', borderBottom: '1px solid #dce8ed', boxSizing: 'border-box' })
  const titleIcon = document.createElement('img')
  titleIcon.src = icon.src
  titleIcon.alt = ''
  applyStyles(titleIcon, { width: '38px', height: '38px', flex: '0 0 auto', padding: '2px', borderRadius: '12px', background: '#e7f6fa', objectFit: 'contain' })
  titlebar.append(titleIcon)
  const titleCopy = document.createElement('span')
  const title = document.createElement('strong')
  title.textContent = config.theme?.title || '知识库助手'
  const subtitle = document.createElement('small')
  subtitle.textContent = '基于授权知识库回答'
  applyStyles(titleCopy, { display: 'grid', minWidth: '0', lineHeight: '1.2' })
  applyStyles(title, { overflow: 'hidden', fontSize: '14px', textOverflow: 'ellipsis', whiteSpace: 'nowrap' })
  applyStyles(subtitle, { marginTop: '3px', overflow: 'hidden', color: '#6b7c86', fontSize: '11px', fontWeight: '400', textOverflow: 'ellipsis', whiteSpace: 'nowrap' })
  titleCopy.append(title, subtitle)
  titlebar.append(titleCopy)

  const newConversationButton = document.createElement('button')
  newConversationButton.className = 'weknora-shell-button'
  newConversationButton.type = 'button'
  newConversationButton.setAttribute('aria-label', '新建会话')
  newConversationButton.textContent = '+'
  applyStyles(newConversationButton, { position: 'absolute', top: '16px', right: '82px', width: '32px', height: '32px', border: '0', borderRadius: '10px', color: '#405560', background: '#edf4f6', pointerEvents: 'auto', cursor: 'pointer', fontSize: '20px', lineHeight: '30px', transition: 'background .16s ease,color .16s ease' })

  const conversationsButton = document.createElement('button')
  conversationsButton.className = 'weknora-shell-button'
  conversationsButton.type = 'button'
  conversationsButton.setAttribute('aria-label', '切换对话')
  conversationsButton.textContent = '☰'
  applyStyles(conversationsButton, { position: 'absolute', top: '16px', right: '118px', width: '32px', height: '32px', border: '0', borderRadius: '10px', color: '#405560', background: '#edf4f6', pointerEvents: 'auto', cursor: 'pointer', fontSize: '16px', transition: 'background .16s ease,color .16s ease' })

  const maximizeButton = document.createElement('button')
  maximizeButton.className = 'weknora-shell-button'
  maximizeButton.type = 'button'
  maximizeButton.setAttribute('aria-label', '最大化聊天窗口')
  const maximizeIcon = '<svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M8 4H4v4M16 4h4v4M20 16v4h-4M8 20H4v-4"/></svg>'
  const restoreIcon = '<svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M9 4H4v5M15 4h5v5M20 15v5h-5M4 15v5h5"/><path d="m4 9 5-5m6 0 5 5m0 6-5 5M9 20l-5-5"/></svg>'
  maximizeButton.innerHTML = maximizeIcon
  applyStyles(maximizeButton, { position: 'absolute', top: '16px', right: '46px', width: '32px', height: '32px', border: '0', borderRadius: '10px', color: '#405560', background: '#edf4f6', pointerEvents: 'auto', cursor: 'pointer', transition: 'background .16s ease,color .16s ease' })

  const closeButton = document.createElement('button')
  closeButton.className = 'weknora-shell-button'
  closeButton.type = 'button'
  closeButton.setAttribute('aria-label', '关闭聊天窗口')
  closeButton.textContent = '×'
  applyStyles(closeButton, { position: 'absolute', top: '16px', right: '10px', width: '32px', height: '32px', border: '0', borderRadius: '10px', color: '#405560', background: '#edf4f6', pointerEvents: 'auto', cursor: 'pointer', fontSize: '18px', transition: 'background .16s ease,color .16s ease' })

  const iframe = document.createElement('iframe')
  iframe.title = config.theme?.title || '知识库聊天内容'
  const iframeURL = new URL(config.iframeUrl)
  iframeURL.searchParams.set('instance_id', config.instanceId)
  iframeURL.searchParams.set('preserve_session', config.preserveSession === false ? 'false' : 'true')
  iframeURL.searchParams.set('parent_origin', window.location.origin)
  iframe.src = iframeURL.href
  iframe.referrerPolicy = 'strict-origin'
  iframe.allow = 'microphone; clipboard-write'
  iframe.setAttribute(
    'sandbox',
    'allow-scripts allow-same-origin allow-forms allow-downloads allow-modals allow-popups allow-popups-to-escape-sandbox',
  )
  applyStyles(iframe, { width: '100%', height: 'calc(100% - 64px)', minWidth: '0', minHeight: '0', border: '0', display: 'block', background: '#f7f9fc' })

  const resizeHandle = document.createElement('div')
  resizeHandle.className = 'weknora-resize-handle'
  resizeHandle.tabIndex = 0
  resizeHandle.setAttribute('role', 'separator')
  resizeHandle.setAttribute('aria-label', '调整聊天窗口大小；方向键缩放')
  applyStyles(resizeHandle, { position: 'absolute', zIndex: '4', right: '0', bottom: '0', width: '30px', height: '30px', cursor: 'nwse-resize', borderRadius: '12px 0 18px 0', background: 'linear-gradient(135deg,transparent 35%,rgba(231,246,250,.94))', touchAction: 'none' })

  panel.append(titlebar, conversationsButton, newConversationButton, maximizeButton, closeButton, iframe, resizeHandle)
  shadow.append(shellStyles, launcher, panel)
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
      applyStyles(panel, { left: `${normal.position.x}px`, top: `${normal.position.y}px`, width: `${normal.size.width}px`, height: `${normal.size.height}px`, borderRadius: '18px' })
      maximizeButton.innerHTML = maximizeIcon
      maximizeButton.setAttribute('aria-label', '最大化聊天窗口')
      sessionStorage.setItem(storageKey, JSON.stringify(normal))
    } else {
      applyStyles(panel, { left: `${viewport.x}px`, top: `${viewport.y}px`, width: `${viewport.width}px`, height: `${viewport.height}px`, borderRadius: '0' })
      maximizeButton.innerHTML = restoreIcon
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

  const pointerDrag = (target: HTMLElement, operation: (dx: number, dy: number) => void, onEnd?: () => void) => {
    target.addEventListener('pointerdown', (event) => {
      if (event.button !== 0 || event.target !== target) return
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
        onEnd?.()
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
  let dockedSide: 'left' | 'right' | null = null
  let launcherMoved = false
  const clampLauncher = () => {
    const viewport = viewportRect()
    const launcherWidth = dockedSide ? DOCKED_LAUNCHER_WIDTH : LAUNCHER_SIZE
    const gapX = dockedSide ? 0 : Math.min(EDGE_GAP, Math.max(0, (viewport.width - launcherWidth) / 2))
    const gapY = Math.min(EDGE_GAP, Math.max(0, (viewport.height - LAUNCHER_SIZE) / 2))
    const maxX = Math.max(viewport.x + gapX, viewport.x + viewport.width - launcherWidth - gapX)
    const maxY = Math.max(viewport.y + gapY, viewport.y + viewport.height - LAUNCHER_SIZE - gapY)
    launcherPosition = {
      x: Math.min(Math.max(launcherPosition.x, viewport.x + gapX), maxX),
      y: Math.min(Math.max(launcherPosition.y, viewport.y + gapY), maxY),
    }
    if (dockedSide) launcher.dataset.docked = dockedSide
    else delete launcher.dataset.docked
    applyStyles(launcher, { left: `${launcherPosition.x}px`, top: `${launcherPosition.y}px`, right: 'auto', bottom: 'auto', width: `${launcherWidth}px` })
  }
  clampLauncher()
  const snapLauncherToEdge = () => {
    if (!launcherMoved) return
    const viewport = viewportRect()
    const leftDistance = launcherPosition.x - viewport.x
    const rightDistance = viewport.x + viewport.width - (launcherPosition.x + LAUNCHER_SIZE)
    if (leftDistance > DOCK_DISTANCE && rightDistance > DOCK_DISTANCE) return
    dockedSide = leftDistance <= rightDistance ? 'left' : 'right'
    launcherPosition.x = dockedSide === 'left'
      ? viewport.x
      : viewport.x + viewport.width - DOCKED_LAUNCHER_WIDTH
    clampLauncher()
  }
  pointerDrag(launcher, (dx, dy) => {
    if (dx === 0 && dy === 0) return
    launcherMoved = true
    if (dockedSide) {
      dockedSide = null
      delete launcher.dataset.docked
      launcher.style.width = `${LAUNCHER_SIZE}px`
    }
    launcherPosition = { x: launcherPosition.x + dx, y: launcherPosition.y + dy }
    clampLauncher()
  }, snapLauncherToEdge)

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
      if (!frameReady) {
        pendingTicket = ticket
        return
      }
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
    on(event, listener) {
      const bucket = listeners.get(event) ?? new Set()
      bucket.add(listener)
      listeners.set(event, bucket)
      if (event === 'ready' && frameReady) {
        queueMicrotask(() => {
          if (!destroyed && bucket.has(listener)) listener({ version: 1, type: 'ready' })
        })
      }
      return () => bucket.delete(listener)
    },
  }

  const onMessage = (event: MessageEvent) => {
    if (event.origin !== config.targetOrigin || event.source !== iframe.contentWindow) return
    const message = parseFrameMessage(event.data)
    if (!message) return
    if (message.type === 'ready') {
      if (frameReady) return
      frameReady = true
      iframe.contentWindow?.postMessage({ version: 1, type: 'configure', selection: config.selection, theme: config.theme }, config.targetOrigin!)
      if (pendingTicket) {
        iframe.contentWindow?.postMessage({ version: 1, type: 'auth-ready', ticket: pendingTicket }, config.targetOrigin!)
        pendingTicket = null
      }
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
  newConversationButton.addEventListener('click', () => iframe.contentWindow?.postMessage({ version: 1, type: 'new-conversation' }, config.targetOrigin!))
  conversationsButton.addEventListener('click', () => iframe.contentWindow?.postMessage({ version: 1, type: 'toggle-conversations' }, config.targetOrigin!))
  maximizeButton.addEventListener('click', () => layout.mode === 'normal' ? instance.maximize() : instance.restore())
  instances.set(config.instanceId, instance)
  render(false)
  return instance
}

export type * from './types'
