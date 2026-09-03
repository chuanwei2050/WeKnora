import hljs from 'highlight.js';
import { marked } from 'marked';
import markedKatex from 'marked-katex-extension';

type TextPreviewRequest = {
  buffer: ArrayBuffer;
  fileType: string;
  mode: 'text' | 'markdown';
};

type TextPreviewResponse =
  | { ok: true; content: string; format: 'html' | 'text'; truncated: boolean }
  | { ok: false; message: string };

const MAX_INPUT_CHARACTERS = 200_000;
const MAX_INPUT_LINES = 5000;
const MAX_HTML_CHARACTERS = 1_000_000;

const languageAliases: Record<string, string> = {
  js: 'javascript', ts: 'typescript', py: 'python', rb: 'ruby',
  sh: 'bash', yml: 'yaml', md: 'markdown', rs: 'rust',
  kt: 'kotlin', pl: 'perl', conf: 'ini', log: 'plaintext',
};

function highlight(text: string, fileType: string): string {
  const language = languageAliases[fileType.toLowerCase()] || fileType.toLowerCase();
  if (language && hljs.getLanguage(language)) {
    try {
      return hljs.highlight(text, { language }).value;
    } catch {
      // Fall back to automatic detection.
    }
  }
  return hljs.highlightAuto(text).value;
}

function renderMarkdown(text: string): string {
  if (!text) {
    return '<p style="color: var(--td-text-color-disabled); text-align: center; padding: 20px;">文档内容为空</p>';
  }
  marked.use({ breaks: true, gfm: true });
  marked.use(markedKatex({ throwOnError: false }));
  const renderer = new marked.Renderer();
  renderer.code = ({ text: code, lang }) => (
    `<pre><code class="hljs">${highlight(code || '', lang || '')}</code></pre>`
  );
  marked.use({ renderer });
  const withoutEmbeds = text
    .replace(/<script\b[^<]*(?:(?!<\/script>)<[^<]*)*<\/script>/gi, '')
    .replace(/<iframe\b[^<]*(?:(?!<\/iframe>)<[^<]*)*<\/iframe>/gi, '')
    .replace(/<object\b[^<]*(?:(?!<\/object>)<[^<]*)*<\/object>/gi, '')
    .replace(/<embed\b[^<]*(?:(?!<\/embed>)<[^<]*)*<\/embed>/gi, '')
    .replace(/\\\[([\s\S]*?)\\\]/g, '$$$$$1$$$$')
    .replace(/\\\(([\s\S]*?)\\\)/g, '$$$1$$');
  return marked.parse(withoutEmbeds) as string;
}

self.onmessage = (event: MessageEvent<TextPreviewRequest>) => {
  try {
    const { buffer, fileType, mode } = event.data;
    const decoded = new TextDecoder('utf-8').decode(buffer);
    let end = Math.min(decoded.length, MAX_INPUT_CHARACTERS);
    let lineCount = 1;
    for (let index = 0; index < end; index++) {
      if (decoded.charCodeAt(index) === 10 && ++lineCount > MAX_INPUT_LINES) {
        end = index;
        break;
      }
    }
    const text = decoded.slice(0, end);
    const truncated = end < decoded.length;
    const html = mode === 'markdown' ? renderMarkdown(text) : highlight(text, fileType);
    const response: TextPreviewResponse = html.length <= MAX_HTML_CHARACTERS
      ? { ok: true, content: html, format: 'html', truncated }
      : { ok: true, content: text, format: 'text', truncated: true };
    self.postMessage(response);
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    self.postMessage({ ok: false, message } satisfies TextPreviewResponse);
  }
};
