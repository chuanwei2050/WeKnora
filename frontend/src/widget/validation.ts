import type { NormalLayout, WidgetConfig, WidgetFrameMessage } from './types'

const SAFE_COLOR = /^#[0-9a-f]{6}$/i
const SAFE_ID = /^[a-zA-Z0-9_-]{1,128}$/
const ALLOWED_PROTOCOLS = new Set(['http:', 'https:'])

export function parseWidgetConfig(input: WidgetConfig): WidgetConfig {
  if (!input || typeof input !== 'object') throw new Error('Widget config is required')
  if (input.version !== 1) throw new Error('Unsupported widget config version')
  if (!/^[a-zA-Z0-9_-]{1,64}$/.test(input.instanceId)) throw new Error('Invalid instanceId')
  const iframe = new URL(input.iframeUrl, window.location.href)
  if (!ALLOWED_PROTOCOLS.has(iframe.protocol)) throw new Error('Invalid iframeUrl protocol')
  const targetOrigin = input.targetOrigin ? new URL(input.targetOrigin).origin : iframe.origin
  if (!input.targetOrigin && iframe.origin !== window.location.origin) throw new Error('iframeUrl must use the host origin proxy')
  if (targetOrigin !== iframe.origin) throw new Error('targetOrigin must match iframe origin')
  if (!input.selection || !['fixed', 'selectable', 'all-allowed'].includes(input.selection.mode)) {
    throw new Error('Invalid knowledge base selection mode')
  }
  if (input.selection.mode === 'fixed' && (!Array.isArray(input.selection.knowledgeBaseIds) || input.selection.knowledgeBaseIds.length === 0)) {
    throw new Error('fixed mode requires knowledgeBaseIds')
  }
  const configuredIds = input.selection.mode === 'fixed'
    ? input.selection.knowledgeBaseIds
    : input.selection.mode === 'selectable'
      ? input.selection.initialKnowledgeBaseIds
      : undefined
  if (configuredIds && (configuredIds.length > 100 || !configuredIds.every((id) => typeof id === 'string' && SAFE_ID.test(id)))) {
    throw new Error('Invalid knowledge base id')
  }
  if (input.selection.mode === 'all-allowed' && ('knowledgeBaseIds' in input.selection || 'initialKnowledgeBaseIds' in input.selection)) {
    throw new Error('all-allowed mode cannot include knowledge base ids')
  }
  if (input.selection.mode === 'selectable' && input.selection.initialKnowledgeBaseIds?.length === 0) {
    throw new Error('selectable initialKnowledgeBaseIds must be non-empty when provided')
  }
  if (input.theme?.primaryColor && !SAFE_COLOR.test(input.theme.primaryColor)) {
    throw new Error('primaryColor must be a six-digit hex color')
  }
  if (input.theme?.title && input.theme.title.length > 80) throw new Error('Widget title is too long')
  for (const value of [input.initialPosition?.x, input.initialPosition?.y, input.initialSize?.width, input.initialSize?.height]) {
    if (value !== undefined && (typeof value !== 'number' || !Number.isFinite(value))) throw new Error('Invalid widget layout value')
  }
  if (input.theme?.iconUrl) {
    const icon = new URL(input.theme.iconUrl, window.location.href)
    if (!ALLOWED_PROTOCOLS.has(icon.protocol)) throw new Error('Invalid iconUrl protocol')
    if (icon.origin !== window.location.origin && icon.origin !== targetOrigin) throw new Error('iconUrl must use the host or target origin')
  }
  return { ...input, iframeUrl: iframe.href, targetOrigin }
}

export function parseFrameMessage(value: unknown): WidgetFrameMessage | null {
  if (!value || typeof value !== 'object') return null
  const message = value as Record<string, unknown>
  if (message.version !== 1 || typeof message.type !== 'string') return null
  switch (message.type) {
    case 'ready':
    case 'unauthorized':
      return { version: 1, type: message.type }
    case 'answer-completed':
    return typeof message.messageId === 'string' && message.messageId.length > 0 && message.messageId.length <= 128
        ? { version: 1, type: message.type, messageId: message.messageId }
        : null
    case 'open-document': {
      const validID = (id: unknown) => typeof id === 'string' && /^[a-zA-Z0-9_-]{1,128}$/.test(id)
      if (!validID(message.knowledgeBaseId) || (message.knowledgeId !== undefined && !validID(message.knowledgeId))) return null
      return {
        version: 1,
        type: message.type,
        knowledgeBaseId: message.knowledgeBaseId as string,
        knowledgeId: message.knowledgeId as string | undefined,
      }
    }
    case 'error':
    return typeof message.code === 'string' && message.code.length <= 128 && typeof message.message === 'string' && message.message.length <= 1024
        ? { version: 1, type: message.type, code: message.code, message: message.message }
        : null
    default:
      return null
  }
}

export function parseStoredLayout(value: string | null): NormalLayout | null {
  if (!value) return null
  try {
    const parsed = JSON.parse(value) as Partial<NormalLayout>
    if (parsed.mode !== 'normal' || !parsed.position || !parsed.size) return null
    const values = [parsed.position.x, parsed.position.y, parsed.size.width, parsed.size.height]
    if (!values.every((item) => typeof item === 'number' && Number.isFinite(item))) return null
    return parsed as NormalLayout
  } catch {
    return null
  }
}
