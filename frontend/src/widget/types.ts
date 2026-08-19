export type KnowledgeBaseSelection =
  | { mode: 'fixed'; knowledgeBaseIds: [string, ...string[]] }
  | { mode: 'selectable'; initialKnowledgeBaseIds?: [string, ...string[]] }
  | { mode: 'all-allowed' }

export type WidgetPosition = { x: number; y: number }
export type WidgetSize = { width: number; height: number }
export type NormalLayout = { mode: 'normal'; position: WidgetPosition; size: WidgetSize }
export type WidgetLayout = NormalLayout | { mode: 'maximized'; restore: NormalLayout }

export type WidgetEventName =
  | 'ready'
  | 'open'
  | 'close'
  | 'layout-changed'
  | 'unauthorized'
  | 'answer-completed'
  | 'error'

export interface WidgetTheme {
  primaryColor?: string
  title?: string
  iconUrl?: string
  colorMode?: 'light' | 'dark'
}

export interface WidgetConfig {
  version: 1
  instanceId: string
  iframeUrl: string
  selection: KnowledgeBaseSelection
  targetOrigin?: string
  initialPosition?: WidgetPosition
  initialSize?: WidgetSize
  theme?: WidgetTheme
  preserveSession?: boolean
}

export interface WidgetInstance {
  authenticate(ticket: string): void
  open(): void
  close(): void
  destroy(): void
  moveTo(position: WidgetPosition): void
  resizeTo(size: WidgetSize): void
  maximize(): void
  restore(): void
  resetLayout(): void
  getLayout(): WidgetLayout
  on(event: WidgetEventName, listener: (detail: unknown) => void): () => void
}

export type WidgetHostMessage =
  | { version: 1; type: 'auth-ready'; ticket: string }
  | { version: 1; type: 'set-theme'; theme: WidgetTheme }
  | { version: 1; type: 'configure'; selection: KnowledgeBaseSelection; theme?: WidgetTheme }

export type WidgetFrameMessage =
  | { version: 1; type: 'ready' }
  | { version: 1; type: 'unauthorized' }
  | { version: 1; type: 'answer-completed'; messageId: string }
  | { version: 1; type: 'error'; code: string; message: string }
