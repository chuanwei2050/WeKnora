export type PreviewType = 'pdf' | 'docx' | 'image' | 'excel' | 'text' | 'markdown' | 'pptx' | 'audio' | 'unsupported';

export interface PreviewCacheEntry {
  previewType: PreviewType;
  blob?: Blob;
  highlightedCode?: string;
  markdownHtml?: string;
  plainTextContent?: string;
  excelHtml?: string;
  excelPreviewTruncated?: boolean;
  textPreviewTruncated?: boolean;
  largeFileBlocked?: boolean;
  pptxData?: ArrayBuffer;
  size: number;
  lastAccessedAt: number;
}

const CACHE_TTL_MS = 10 * 60 * 1000;
const CACHE_MAX_ENTRIES = 2;
const CACHE_MAX_BYTES = 96 * 1024 * 1024;
const CACHE_MAX_ITEM_BYTES = 64 * 1024 * 1024;

const cache = new Map<string, PreviewCacheEntry>();
let expiryTimer: ReturnType<typeof setTimeout> | null = null;

export function buildPreviewCacheKey(knowledgeId: string, fileType: string, contentRevision: string) {
  return `${knowledgeId}:${fileType.toLowerCase()}:${contentRevision}`;
}

export function buildPreviewContentRevision(parts: {
  fileHash?: string;
  currentVersionId?: string;
  pendingVersionId?: string;
  updatedAt?: string;
}) {
  return [parts.fileHash, parts.currentVersionId, parts.pendingVersionId, parts.updatedAt]
    .filter(Boolean)
    .join(':');
}

function prune(now = Date.now()) {
  for (const [key, entry] of cache) {
    if (now - entry.lastAccessedAt >= CACHE_TTL_MS) cache.delete(key);
  }

  let totalBytes = Array.from(cache.values()).reduce((total, entry) => total + entry.size, 0);
  const oldestFirst = Array.from(cache.entries()).sort((a, b) => a[1].lastAccessedAt - b[1].lastAccessedAt);
  for (const [key, entry] of oldestFirst) {
    if (cache.size <= CACHE_MAX_ENTRIES && totalBytes <= CACHE_MAX_BYTES) break;
    cache.delete(key);
    totalBytes -= entry.size;
  }
}

function scheduleCleanup() {
  if (expiryTimer) clearTimeout(expiryTimer);
  if (cache.size === 0) {
    expiryTimer = null;
    return;
  }

  const nextExpiryAt = Math.min(...Array.from(cache.values()).map(entry => entry.lastAccessedAt + CACHE_TTL_MS));
  expiryTimer = setTimeout(() => {
    expiryTimer = null;
    prune();
    scheduleCleanup();
  }, Math.max(0, nextExpiryAt - Date.now()));
}

export function getCachedPreview(key: string) {
  prune();
  const entry = cache.get(key);
  if (!entry) return null;
  entry.lastAccessedAt = Date.now();
  scheduleCleanup();
  return entry;
}

export function setCachedPreview(key: string, entry: Omit<PreviewCacheEntry, 'lastAccessedAt'>) {
  if (entry.size > CACHE_MAX_ITEM_BYTES) return;
  cache.set(key, { ...entry, lastAccessedAt: Date.now() });
  prune();
  scheduleCleanup();
}
