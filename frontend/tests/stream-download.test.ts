import { afterEach, describe, expect, it, vi } from 'vitest';

const { getAuthenticatedRequestHeaders, isCookieEmbeddedMode } = vi.hoisted(() => ({
  getAuthenticatedRequestHeaders: vi.fn(() => ({ Authorization: 'Bearer test-token' })),
  isCookieEmbeddedMode: vi.fn(() => false),
}));

vi.mock('../src/utils/request', () => ({ getAuthenticatedRequestHeaders }));
vi.mock('../src/utils/embedded-runtime', () => ({ isCookieEmbeddedMode }));

import { streamAuthenticatedFileToDisk } from '../src/utils/stream-download';

describe('streamAuthenticatedFileToDisk', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    delete (window as Window & { showSaveFilePicker?: unknown }).showSaveFilePicker;
  });

  it('streams an authenticated response directly to the selected file', async () => {
    let writtenBytes = 0;
    const writable = {
      write: vi.fn(async (chunk: Uint8Array) => { writtenBytes += chunk.byteLength; }),
      close: vi.fn().mockResolvedValue(undefined),
      abort: vi.fn().mockResolvedValue(undefined),
    };
    const createWritable = vi.fn().mockResolvedValue(writable);
    const showSaveFilePicker = vi.fn().mockResolvedValue({ createWritable });
    Object.defineProperty(window, 'showSaveFilePicker', {
      configurable: true,
      value: showSaveFilePicker,
    });
    const fetchMock = vi.fn().mockResolvedValue(new Response('large-file'));
    vi.stubGlobal('fetch', fetchMock);

    await expect(streamAuthenticatedFileToDisk('/api/download', '资料.docx')).resolves.toBe(true);

    expect(showSaveFilePicker).toHaveBeenCalledWith({ suggestedName: '资料.docx' });
    expect(fetchMock).toHaveBeenCalledWith('/api/download', {
      headers: { Authorization: 'Bearer test-token' },
      credentials: 'same-origin',
    });
    expect(writtenBytes).toBe(10);
    expect(writable.close).toHaveBeenCalledOnce();
  });

  it('falls back when the browser has no file-system picker', async () => {
    await expect(streamAuthenticatedFileToDisk('/api/download', '资料.docx')).resolves.toBe(false);
  });
});
