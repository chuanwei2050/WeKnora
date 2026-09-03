// @ts-nocheck
<script setup lang="ts">
import { ref, shallowRef, watch, onUnmounted, nextTick, defineAsyncComponent } from 'vue';
import { previewKnowledgeFile } from '@/api/knowledge-base/index';
import 'highlight.js/styles/github.css';
import 'katex/dist/katex.min.css';
import { useI18n } from 'vue-i18n';
import { sanitizeHTML } from '@/utils/security';
import { getCachedPreview, setCachedPreview, type PreviewType } from '@/utils/documentPreviewCache';


const VueOfficePptx = defineAsyncComponent(() => import('@vue-office/pptx'));

const { t } = useI18n();

const props = defineProps<{
  knowledgeId: string;
  fileType: string;
  fileName: string;
  active: boolean;
}>();
const emit = defineEmits<{ switchToChunks: [] }>();

const MAX_TEXT_PREVIEW_BYTES = 2 * 1024 * 1024;

const loading = ref(false);
const error = ref('');
const previewType = ref<PreviewType>('unsupported');
const blobUrl = ref('');
const highlightedCode = ref('');
const markdownHtml = ref('');
const plainTextContent = ref('');
const excelHtml = ref('');
const excelPreviewTruncated = ref(false);
const textPreviewTruncated = ref(false);
const largeFileBlocked = ref(false);
const pptxData = shallowRef<ArrayBuffer | null>(null);
const docxContainer = ref<HTMLElement | null>(null);
const imageNaturalWidth = ref(0);
const imageNaturalHeight = ref(0);
let loadedForId = '';
let excelPreviewWorker: Worker | null = null;
let cancelExcelPreview: (() => void) | null = null;
let textPreviewWorker: Worker | null = null;
let cancelTextPreview: (() => void) | null = null;
let officeSafetyWorker: Worker | null = null;
let cancelOfficeSafetyCheck: (() => void) | null = null;
let previewLoadVersion = 0;

const isFullscreen = ref(false);

function toggleFullscreen() {
  isFullscreen.value = !isFullscreen.value;
  if (isFullscreen.value) {
    document.body.style.overflow = 'hidden';
  } else {
    document.body.style.overflow = '';
  }
}


const fileTypeMap: Record<string, typeof previewType.value> = {};
['pdf'].forEach(t => fileTypeMap[t] = 'pdf');
['docx'].forEach(t => fileTypeMap[t] = 'docx');
['pptx', 'ppt'].forEach(t => fileTypeMap[t] = 'pptx');
['jpg', 'jpeg', 'png', 'gif', 'bmp', 'webp', 'tiff', 'svg'].forEach(t => fileTypeMap[t] = 'image');
['xlsx', 'xls', 'csv'].forEach(t => fileTypeMap[t] = 'excel');
['md', 'markdown'].forEach(t => fileTypeMap[t] = 'markdown');
['txt', 'json', 'xml', 'html', 'css', 'js', 'ts', 'py', 'java', 'go',
 'cpp', 'c', 'h', 'sh', 'yaml', 'yml', 'ini', 'conf', 'log', 'sql', 'rs', 'rb', 'php',
 'swift', 'kt', 'scala', 'r', 'lua', 'pl', 'toml'].forEach(t => fileTypeMap[t] = 'text');
['mp3', 'wav', 'm4a', 'flac', 'ogg'].forEach(t => fileTypeMap[t] = 'audio');

const mimeTypeMap: Record<string, string> = {
  pdf: 'application/pdf',
  docx: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  doc: 'application/msword',
  pptx: 'application/vnd.openxmlformats-officedocument.presentationml.presentation',
  ppt: 'application/vnd.ms-powerpoint',
  xlsx: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  xls: 'application/vnd.ms-excel',
  csv: 'text/csv',
  jpg: 'image/jpeg', jpeg: 'image/jpeg',
  png: 'image/png', gif: 'image/gif', bmp: 'image/bmp',
  webp: 'image/webp', tiff: 'image/tiff', svg: 'image/svg+xml',
  txt: 'text/plain', md: 'text/markdown', markdown: 'text/markdown',
  json: 'application/json', xml: 'application/xml',
  html: 'text/html', css: 'text/css',
  js: 'text/javascript', ts: 'text/typescript',
  py: 'text/x-python', java: 'text/x-java', go: 'text/x-go',
  mp3: 'audio/mpeg', wav: 'audio/wav', m4a: 'audio/mp4',
  flac: 'audio/flac', ogg: 'audio/ogg',
};

function getMimeType(ft: string): string {
  return mimeTypeMap[ft?.toLowerCase()] || 'application/octet-stream';
}

function ensureBlobType(blob: Blob, ft: string): Blob {
  const expected = getMimeType(ft);
  if (blob.type === expected) return blob;
  return new Blob([blob], { type: expected });
}

function resolvePreviewType(ft: string): typeof previewType.value {
  return fileTypeMap[ft?.toLowerCase()] || 'unsupported';
}

async function renderDocx(blob: Blob) {
  const { renderAsync } = await import('docx-preview');
  if (docxContainer.value) {
    docxContainer.value.innerHTML = '';
    await renderAsync(blob, docxContainer.value, undefined, {
      className: 'docx-preview-wrapper',
      inWrapper: true,
      ignoreWidth: false,
      ignoreHeight: false,
      ignoreFonts: false,
      breakPages: true,
      ignoreLastRenderedPageBreak: true,
      experimental: false,
      trimXmlDeclaration: true,
      useBase64URL: true,
    });
  }
}

async function renderExcel(blob: Blob, fileType: string | undefined, isCurrent: () => boolean) {
  const arrayBuffer = await blob.arrayBuffer();
  if (!isCurrent()) return null;
  const worker = new Worker(new URL('../workers/excel-preview.worker.ts', import.meta.url), { type: 'module' });
  excelPreviewWorker = worker;
  let cancelled = false;
  let cancelCurrentPreview = () => {};
  const result = await new Promise<{ html: string; truncated: boolean }>((resolve, reject) => {
    cancelCurrentPreview = () => {
      cancelled = true;
      reject(new Error('Excel preview cancelled'));
    };
    cancelExcelPreview = cancelCurrentPreview;
    worker.onmessage = (event) => {
      if (event.data?.ok) resolve(event.data);
      else reject(new Error(event.data?.message || 'Excel preview failed'));
    };
    worker.onerror = (event) => reject(new Error(event.message || 'Excel preview failed'));
    worker.postMessage({ buffer: arrayBuffer, fileType: fileType || '' }, [arrayBuffer]);
  }).catch((error) => {
    if (cancelled) return null;
    throw error;
  }).finally(() => {
    worker.terminate();
    if (excelPreviewWorker === worker) excelPreviewWorker = null;
    if (cancelExcelPreview === cancelCurrentPreview) cancelExcelPreview = null;
  });
  return result;
}

async function renderTextPreview(blob: Blob, fileType: string, mode: 'text' | 'markdown', isCurrent: () => boolean) {
  const truncated = blob.size > MAX_TEXT_PREVIEW_BYTES;
  const buffer = await blob.slice(0, MAX_TEXT_PREVIEW_BYTES).arrayBuffer();
  if (!isCurrent()) return null;
  const worker = new Worker(new URL('../workers/text-preview.worker.ts', import.meta.url), { type: 'module' });
  textPreviewWorker = worker;
  let cancelled = false;
  let cancelCurrentPreview = () => {};
  const result = await new Promise<{ content: string; format: 'html' | 'text'; truncated: boolean }>((resolve, reject) => {
    cancelCurrentPreview = () => {
      cancelled = true;
      reject(new Error('Text preview cancelled'));
    };
    cancelTextPreview = cancelCurrentPreview;
    worker.onmessage = (event) => {
      if (event.data?.ok) resolve(event.data);
      else reject(new Error(event.data?.message || 'Text preview failed'));
    };
    worker.onerror = (event) => reject(new Error(event.message || 'Text preview failed'));
    worker.postMessage({ buffer, fileType, mode }, [buffer]);
  }).catch((error) => {
    if (cancelled) return null;
    throw error;
  }).finally(() => {
    worker.terminate();
    if (textPreviewWorker === worker) textPreviewWorker = null;
    if (cancelTextPreview === cancelCurrentPreview) cancelTextPreview = null;
  });
  return result ? { ...result, truncated: truncated || result.truncated } : null;
}

async function isOfficePreviewSafe(blob: Blob, isCurrent: () => boolean) {
  const worker = new Worker(new URL('../workers/office-preview-safety.worker.ts', import.meta.url), { type: 'module' });
  officeSafetyWorker = worker;
  let cancelled = false;
  let cancelCurrentCheck = () => {};
  const result = await new Promise<{ safe: boolean }>((resolve, reject) => {
    cancelCurrentCheck = () => {
      cancelled = true;
      reject(new Error('Office safety check cancelled'));
    };
    cancelOfficeSafetyCheck = cancelCurrentCheck;
    worker.onmessage = (event) => {
      if (event.data?.ok) resolve(event.data);
      else reject(new Error(event.data?.message || 'Office safety check failed'));
    };
    worker.onerror = (event) => reject(new Error(event.message || 'Office safety check failed'));
    worker.postMessage({ blob });
  }).catch((error) => {
    if (cancelled) return null;
    throw error;
  }).finally(() => {
    worker.terminate();
    if (officeSafetyWorker === worker) officeSafetyWorker = null;
    if (cancelOfficeSafetyCheck === cancelCurrentCheck) cancelOfficeSafetyCheck = null;
  });
  return result && isCurrent() ? result.safe : false;
}

function onImageLoad(e: Event) {
  const img = e.target as HTMLImageElement;
  imageNaturalWidth.value = img.naturalWidth;
  imageNaturalHeight.value = img.naturalHeight;
}

async function loadPreview() {
  const id = props.knowledgeId;
  const ft = props.fileType;
  if (!id || !ft) return;
  if (loadedForId === id) return;

  cleanup();
  const loadVersion = previewLoadVersion;
  const isCurrent = () => loadVersion === previewLoadVersion;
  loading.value = true;
  error.value = '';
  previewType.value = resolvePreviewType(ft);
  const cacheKey = `${id}:${ft.toLowerCase()}`;

  if (previewType.value === 'unsupported') {
    loading.value = false;
    return;
  }

  try {
    const cached = getCachedPreview(cacheKey);
    if (cached) {
      previewType.value = cached.previewType;
      highlightedCode.value = cached.highlightedCode || '';
      markdownHtml.value = cached.markdownHtml || '';
      plainTextContent.value = cached.plainTextContent || '';
      excelHtml.value = cached.excelHtml || '';
      excelPreviewTruncated.value = Boolean(cached.excelPreviewTruncated);
      textPreviewTruncated.value = Boolean(cached.textPreviewTruncated);
      largeFileBlocked.value = Boolean(cached.largeFileBlocked);
      pptxData.value = cached.pptxData || null;
      loadedForId = id;

      if (cached.blob) {
        if (cached.previewType === 'docx') {
          loading.value = false;
          await nextTick();
          if (isCurrent()) await renderDocx(cached.blob);
          return;
        }
        blobUrl.value = URL.createObjectURL(cached.blob);
      }
      return;
    }

    const rawBlob = await previewKnowledgeFile(id);
    if (!isCurrent()) return;
    const blob = ensureBlobType(rawBlob, ft);
    loadedForId = id;

    if (previewType.value === 'docx' || previewType.value === 'pptx') {
      const safe = await isOfficePreviewSafe(blob, isCurrent);
      if (!isCurrent()) return;
      if (!safe) {
        largeFileBlocked.value = true;
        setCachedPreview(cacheKey, { previewType: previewType.value, largeFileBlocked: true, size: 1 });
        return;
      }
    }

    // docx-preview needs its container mounted before rendering. Other formats,
    // especially large spreadsheets, keep the loading state through parsing.
    if (previewType.value === 'docx') {
      loading.value = false;
      await nextTick();
      if (!isCurrent()) return;
      await renderDocx(blob);
      if (isCurrent()) setCachedPreview(cacheKey, { previewType: previewType.value, blob, size: blob.size });
      return;
    }

    switch (previewType.value) {
      case 'pdf': {
        blobUrl.value = URL.createObjectURL(blob);
        setCachedPreview(cacheKey, { previewType: previewType.value, blob, size: blob.size });
        break;
      }
      case 'image': {
        blobUrl.value = URL.createObjectURL(blob);
        setCachedPreview(cacheKey, { previewType: previewType.value, blob, size: blob.size });
        break;
      }
      case 'excel': {
        const result = await renderExcel(blob, ft, isCurrent);
        if (!result || !isCurrent()) return;
        excelPreviewTruncated.value = result.truncated;
        excelHtml.value = sanitizeHTML(result.html);
        setCachedPreview(cacheKey, {
          previewType: previewType.value,
          excelHtml: excelHtml.value,
          excelPreviewTruncated: result.truncated,
          size: excelHtml.value.length * 2,
        });
        break;
      }
      case 'text': {
        const result = await renderTextPreview(blob, ft, 'text', isCurrent);
        if (!result || !isCurrent()) return;
        if (result.format === 'html') highlightedCode.value = result.content;
        else plainTextContent.value = result.content;
        textPreviewTruncated.value = result.truncated;
        setCachedPreview(cacheKey, {
          previewType: previewType.value,
          highlightedCode: highlightedCode.value,
          plainTextContent: plainTextContent.value,
          textPreviewTruncated: result.truncated,
          size: result.content.length * 2,
        });
        break;
      }
      case 'markdown': {
        const result = await renderTextPreview(blob, ft, 'markdown', isCurrent);
        if (!result || !isCurrent()) return;
        if (result.format === 'html') markdownHtml.value = sanitizeHTML(result.content);
        else plainTextContent.value = result.content;
        textPreviewTruncated.value = result.truncated;
        setCachedPreview(cacheKey, {
          previewType: previewType.value,
          markdownHtml: markdownHtml.value,
          plainTextContent: plainTextContent.value,
          textPreviewTruncated: result.truncated,
          size: (markdownHtml.value.length + plainTextContent.value.length) * 2,
        });
        break;
      }
      case 'pptx': {
        const data = await blob.arrayBuffer();
        if (!isCurrent()) return;
        pptxData.value = data;
        setCachedPreview(cacheKey, { previewType: previewType.value, pptxData: data, size: data.byteLength });
        break;
      }
      case 'audio': {
        blobUrl.value = URL.createObjectURL(blob);
        setCachedPreview(cacheKey, { previewType: previewType.value, blob, size: blob.size });
        break;
      }
    }
  } catch (err: any) {
    if (!isCurrent()) return;
    console.error('Document preview failed:', err);
    error.value = err?.message || t('preview.loadFailed');
  } finally {
    if (isCurrent()) loading.value = false;
  }
}

function cleanup() {
  previewLoadVersion += 1;
  cancelOfficeSafetyCheck?.();
  cancelOfficeSafetyCheck = null;
  officeSafetyWorker?.terminate();
  officeSafetyWorker = null;
  cancelTextPreview?.();
  cancelTextPreview = null;
  textPreviewWorker?.terminate();
  textPreviewWorker = null;
  cancelExcelPreview?.();
  cancelExcelPreview = null;
  if (excelPreviewWorker) {
    excelPreviewWorker.terminate();
    excelPreviewWorker = null;
  }
  if (blobUrl.value) {
    URL.revokeObjectURL(blobUrl.value);
    blobUrl.value = '';
  }
  highlightedCode.value = '';
  markdownHtml.value = '';
  plainTextContent.value = '';
  excelHtml.value = '';
  excelPreviewTruncated.value = false;
  textPreviewTruncated.value = false;
  largeFileBlocked.value = false;
  pptxData.value = null;
  imageNaturalWidth.value = 0;
  imageNaturalHeight.value = 0;
  loadedForId = '';
  if (docxContainer.value) {
    docxContainer.value.innerHTML = '';
  }
}

watch(
  () => [props.active, props.knowledgeId],
  ([active]) => {
    if (active && props.knowledgeId) {
      loadPreview();
    }
  },
  { immediate: true }
);

onUnmounted(() => {
  document.body.style.overflow = '';
  cleanup();
});
</script>

<template>
  <div class="document-preview" :class="{ 'is-fullscreen': isFullscreen }">
    <!-- Toolbar -->
    <div class="preview-toolbar" v-if="!loading && !error && previewType !== 'unsupported'">
      <t-space size="small">
        <t-tooltip :content="isFullscreen ? $t('preview.exitFullscreen') : $t('preview.fullscreen')" placement="bottom">
          <t-button theme="default" variant="text" shape="square" @click="toggleFullscreen">
            <template #icon><t-icon :name="isFullscreen ? 'fullscreen-exit' : 'fullscreen'" /></template>
          </t-button>
        </t-tooltip>
      </t-space>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="preview-loading">
      <t-loading size="medium" />
      <span class="loading-text">{{ $t('preview.loading') }}</span>
    </div>

    <!-- Error -->
    <div v-else-if="error" class="preview-error">
      <t-icon name="error-circle" size="48px" />
      <p>{{ error }}</p>
      <t-button theme="primary" size="small" @click="loadedForId = ''; loadPreview()">
        {{ $t('preview.retry') }}
      </t-button>
    </div>

    <div v-else-if="largeFileBlocked" class="preview-unsupported">
      <t-icon name="info-circle" size="48px" />
      <p>{{ $t('preview.largeFileBlocked') }}</p>
      <p class="unsupported-hint">{{ $t('preview.largeFileHint') }}</p>
      <t-button theme="primary" size="small" @click="emit('switchToChunks')">
        {{ $t('knowledgeBase.viewChunks') }}
      </t-button>
    </div>

    <!-- Unsupported -->
    <div v-else-if="previewType === 'unsupported'" class="preview-unsupported">
      <t-icon name="file-unknown" size="48px" />
      <p>{{ $t('preview.unsupported') }}</p>
      <p class="unsupported-hint">{{ $t('preview.unsupportedHint') }}</p>
    </div>

    <!-- PDF -->
    <div v-else-if="previewType === 'pdf' && blobUrl" class="preview-pdf">
      <iframe :src="blobUrl" class="pdf-iframe" />
    </div>

    <!-- Image -->
    <div v-else-if="previewType === 'image' && blobUrl" class="preview-image">
      <div class="image-wrapper">
        <img :src="blobUrl" :alt="fileName" @load="onImageLoad" />
        <div v-if="imageNaturalWidth" class="image-info">
          {{ imageNaturalWidth }} × {{ imageNaturalHeight }} px
        </div>
      </div>
    </div>

    <!-- DOCX -->
    <div v-else-if="previewType === 'docx'" class="preview-docx">
      <div ref="docxContainer" class="docx-container" />
    </div>

    <!-- PPTX -->
    <div v-else-if="previewType === 'pptx' && pptxData" class="preview-pptx">
      <vue-office-pptx :src="pptxData" @rendered="() => {}" @error="(e: any) => { error = e?.message || $t('preview.loadFailed'); }" />
    </div>

    <!-- Excel -->
    <div v-else-if="previewType === 'excel' && excelHtml" class="preview-excel">
      <div v-if="excelPreviewTruncated" class="excel-preview-notice">
        <t-icon name="info-circle" size="16px" />
        <span>{{ $t('preview.excelTruncated') }}</span>
      </div>
      <div class="excel-container" v-html="excelHtml" />
    </div>

    <!-- Markdown -->
    <div v-else-if="previewType === 'markdown' && (markdownHtml || plainTextContent)" class="preview-markdown">
      <div v-if="textPreviewTruncated" class="excel-preview-notice">{{ $t('preview.textTruncated') }}</div>
      <pre v-if="plainTextContent" class="code-preview"><code>{{ plainTextContent }}</code></pre>
      <div v-else class="markdown-body" v-html="markdownHtml" />
    </div>

    <!-- Text / Code -->
    <div v-else-if="previewType === 'text' && (highlightedCode || plainTextContent)" class="preview-text">
      <div v-if="textPreviewTruncated" class="excel-preview-notice">{{ $t('preview.textTruncated') }}</div>
      <pre class="code-preview"><code v-if="plainTextContent">{{ plainTextContent }}</code><code v-else class="hljs" v-html="highlightedCode"></code></pre>
    </div>

    <!-- Audio -->
    <div v-else-if="previewType === 'audio' && blobUrl" class="preview-audio">
      <div class="audio-wrapper">
        <t-icon name="sound" size="48px" />
        <p class="audio-filename">{{ fileName }}</p>
        <audio controls :src="blobUrl" class="audio-element">
          {{ $t('preview.audioNotSupported') }}
        </audio>
      </div>
    </div>
  </div>
</template>

<style scoped lang="less">
// ── Design tokens ──
@border-color: var(--td-component-stroke);
@border-radius: 6px;
@bg-white: var(--td-bg-color-container);
@bg-subtle: var(--td-bg-color-container);
@bg-muted: var(--td-bg-color-secondarycontainer);
@text-primary: var(--td-text-color-primary);
@text-secondary: var(--td-text-color-secondary);
@text-tertiary: var(--td-text-color-placeholder);
@text-disabled: var(--td-text-color-disabled);
@accent: var(--td-brand-color);
@accent-hover: var(--td-brand-color-active);
@accent-bg: var(--td-success-color-light);
@accent-bg-hover: var(--td-success-color-light);
@error-color: var(--td-error-color);
@table-border: var(--td-component-stroke);
@preview-max-h: calc(100vh - 200px);
@transition: all 0.2s ease;

// ── Shared container mixin ──
.preview-container() {
  border: 1px solid @border-color;
  border-radius: @border-radius;
  overflow: auto;
  max-height: @preview-max-h;
  background: @bg-white;
}

.document-preview {
  min-height: 200px;
  position: relative;
}

.is-fullscreen {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  z-index: 2001;
  background: var(--td-bg-color-container);
  padding: 0;
  overflow-y: auto;

  .preview-toolbar {
    position: fixed;
    top: 12px;
    right: 32px;
    z-index: 2002;
  }

  .preview-pdf {
    height: 100vh;
  }

  .preview-pptx {
    height: auto;
    min-height: 100vh;
    overflow: visible;
    border: none;

    :deep(.pptx-preview-wrapper) {
      height: auto !important;
      overflow-y: visible !important;
    }
  }

  .preview-docx {
    height: 100vh;
    display: flex;
    flex-direction: column;
    .docx-container {
      max-height: 100vh;
      height: 100%;
      flex: 1;
    }
  }

  .preview-image {
    min-height: 100vh;
    display: flex;
    justify-content: center;
    align-items: center;
    .image-wrapper img {
      max-height: calc(100vh - 80px);
    }
  }

  .preview-excel .excel-container,
  .preview-markdown,
  .preview-text .code-preview {
    max-height: 100vh;
  }
}

.preview-toolbar {
  position: absolute;
  top: 8px;
  right: 24px;
  z-index: 10;
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-border);
  border-radius: var(--td-radius-default);
  box-shadow: var(--td-shadow-1);
  padding: 4px;
  opacity: 0.6;
  transition: opacity 0.2s;

  &:hover {
    opacity: 1;
  }
}

// ── States ──
.preview-loading {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 60px 20px;
  gap: 16px;
  .loading-text { color: @text-tertiary; font-size: 14px; }
}

.preview-error {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 60px 20px;
  gap: 12px;
  color: @error-color;
  p { margin: 0; font-size: 14px; color: @text-secondary; }
}

.preview-unsupported {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 60px 20px;
  gap: 12px;
  color: @text-disabled;
  p { margin: 0; font-size: 14px; color: @text-secondary; }
  .unsupported-hint { font-size: 12px; color: @text-tertiary; }
}

// ── PDF ──
.preview-pdf {
  width: 100%;
  height: @preview-max-h;
  min-height: 500px;
  .pdf-iframe {
    width: 100%;
    height: 100%;
    border: none;
    border-radius: @border-radius;
  }
}

// ── Image ──
.preview-image {
  display: flex;
  justify-content: center;
  padding: 20px 0;
  .image-wrapper {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    img {
      max-width: 100%;
      max-height: calc(100vh - 280px);
      border-radius: @border-radius;
      box-shadow: 0 2px 12px rgba(23, 74, 124, 0.08);
      object-fit: contain;
    }
    .image-info { font-size: 12px; color: @text-tertiary; }
  }
}

// ── Markdown ──
.preview-markdown {
  .preview-container();
  padding: 20px 24px;
}

// ── DOCX ──
.preview-docx {
  .docx-container { .preview-container(); }
}

// ── PPTX ──
.preview-pptx {
  max-height: @preview-max-h;
  min-height: 500px;
  border: 1px solid @border-color;
  border-radius: @border-radius;
  overflow: auto;
  background: @bg-subtle;

  :deep(.pptx-preview-wrapper) {
    height: auto !important;
    overflow-y: visible !important;
  }
}

// ── Excel ──
.preview-excel {
  .excel-preview-notice {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 8px;
    padding: 8px 12px;
    border: 1px solid var(--td-warning-color-3);
    border-radius: @border-radius;
    background: var(--td-warning-color-1);
    color: var(--td-text-color-secondary);
    font-size: 12px;
  }
  .excel-container { .preview-container(); }
}

// ── Text / Code ──
.preview-text {
  .code-preview {
    .preview-container();
    margin: 0;
    padding: 16px;
    background: @bg-subtle;
    font-size: 13px;
    line-height: 1.6;
    code {
      white-space: pre;
      word-wrap: normal;
      display: block;
      background: transparent;
    }
  }
}

// ── Audio ──
.preview-audio {
  display: flex;
  justify-content: center;
  padding: 40px 20px;
  .audio-wrapper {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 16px;
    color: @text-secondary;
    .audio-filename { font-size: 14px; color: @text-primary; margin: 0; }
    .audio-element { width: 100%; max-width: 480px; }
  }
}

// ── Deep styles (v-html / third-party components) ──

// Shared table mixin for v-html content
.preview-table() {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
  th, td {
    border: 1px solid @table-border;
    padding: 6px 12px;
    text-align: left;
  }
  th {
    background: @accent-bg;
    font-weight: 600;
    color: @text-primary;
  }
  tr:hover td {
    background: @accent-bg;
    transition: @transition;
  }
}

:deep(.markdown-body) {
  font-size: 14px;
  line-height: 1.7;
  color: @text-primary;
  word-break: break-word;

  h1, h2, h3, h4, h5, h6 {
    margin-top: 20px;
    margin-bottom: 10px;
    font-weight: 600;
    line-height: 1.4;
  }
  h1 { font-size: 24px; border-bottom: 1px solid @border-color; padding-bottom: 8px; }
  h2 { font-size: 20px; border-bottom: 1px solid @border-color; padding-bottom: 6px; }
  h3 { font-size: 17px; }

  p { margin: 8px 0; }
  blockquote {
    margin: 12px 0;
    padding: 8px 16px;
    border-left: 4px solid @accent;
    background: @bg-subtle;
    color: var(--td-text-color-secondary);
  }
  ul, ol { padding-left: 24px; margin: 8px 0; }
  li { margin: 4px 0; }

  table { .preview-table(); margin: 12px 0; }

  pre {
    margin: 12px 0;
    padding: 14px;
    background: @bg-subtle;
    border-radius: @border-radius;
    overflow: auto;
    font-size: 13px;
    line-height: 1.5;
    code { background: transparent; padding: 0; }
  }
  code {
    background: var(--td-bg-color-secondarycontainer);
    padding: 2px 6px;
    border-radius: 3px;
    font-size: 0.9em;
  }
  img { max-width: 100%; border-radius: 4px; }
  hr { border: none; border-top: 1px solid @border-color; margin: 20px 0; }
  a { color: @accent; text-decoration: none; &:hover { color: @accent-hover; text-decoration: underline; } }
  strong { font-weight: 600; }
}

:deep(.docx-preview-wrapper) {
  padding: 20px;
  max-width: 100%;
  width: 100%;
  box-sizing: border-box;
  overflow-x: auto; // 如果内容过宽，允许水平滚动而不是溢出
  
  // 约束所有子元素的宽度
  * {
    max-width: 100%;
    box-sizing: border-box;
  }
  
  // 特别处理表格
  table {
    width: 100%;
    table-layout: auto;
    word-wrap: break-word;
  }
  
  // 处理图片
  img {
    max-width: 100%;
    height: auto;
  }
  
  // 处理可能的固定宽度元素
  [style*="width"] {
    max-width: 100% !important;
  }
}

:deep(.vue-office-pptx) {
  width: 100%;
  min-height: 100%;
}

:deep(.vue-office-pptx-main) {
  width: 100%;
  min-height: 100%;
}

:deep(.excel-sheet) {
  padding: 0;
  .excel-sheet-name {
    position: sticky;
    top: 0;
    background: @accent-bg;
    padding: 8px 16px;
    font-weight: 600;
    font-size: 13px;
    color: @text-primary;
    border-bottom: 1px solid @border-color;
    z-index: 1;
  }
  table {
    .preview-table();
    th, td {
      white-space: nowrap;
      max-width: 300px;
      overflow: hidden;
      text-overflow: ellipsis;
    }
  }
}
</style>
