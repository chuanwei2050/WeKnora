import { isCookieEmbeddedMode } from '@/utils/embedded-runtime';
import { getAuthenticatedRequestHeaders } from '@/utils/request';

type DownloadPickerWindow = Window & {
  showSaveFilePicker?: (options: { suggestedName: string }) => Promise<FileSystemFileHandle>;
};

export type DownloadProgress = { loaded: number; total: number | null };

export async function streamAuthenticatedFileToDisk(
  url: string,
  filename: string,
  onProgress?: (progress: DownloadProgress) => void,
): Promise<boolean> {
  const pickerWindow = window as DownloadPickerWindow;
  if (!pickerWindow.showSaveFilePicker) return false;

  // The picker must open directly from the click. After permission is granted,
  // each response chunk is written to disk without assembling a browser Blob.
  const handle = await pickerWindow.showSaveFilePicker({ suggestedName: filename });
  const writable = await handle.createWritable();
  try {
    const response = await fetch(url, {
      headers: getAuthenticatedRequestHeaders(url),
      credentials: isCookieEmbeddedMode() ? 'include' : 'same-origin',
    });
    if (!response.ok || !response.body) {
      throw new Error(`Download failed with HTTP ${response.status}`);
    }
    const contentLength = Number(response.headers.get('Content-Length'));
    const total = Number.isFinite(contentLength) && contentLength > 0 ? contentLength : null;
    const reader = response.body.getReader();
    let loaded = 0;
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      await writable.write(value);
      loaded += value.byteLength;
      onProgress?.({ loaded, total });
    }
    await writable.close();
    return true;
  } catch (error) {
    await writable.abort(error);
    throw error;
  }
}
