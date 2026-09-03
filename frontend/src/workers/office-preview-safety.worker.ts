type OfficeSafetyResponse =
  | { ok: true; safe: boolean }
  | { ok: false; message: string };

const MAX_EXPANDED_BYTES = 20 * 1024 * 1024;
const MAX_ARCHIVE_ENTRIES = 1000;
const MAX_COMPRESSION_RATIO = 20;
const EOCD_SIGNATURE = 0x06054b50;
const CENTRAL_FILE_SIGNATURE = 0x02014b50;

self.onmessage = async (event: MessageEvent<{ blob: Blob }>) => {
  try {
    const buffer = await event.data.blob.arrayBuffer();
    const view = new DataView(buffer);
    const searchStart = Math.max(0, view.byteLength - 65_557);
    let eocdOffset = -1;
    for (let offset = view.byteLength - 22; offset >= searchStart; offset--) {
      if (view.getUint32(offset, true) === EOCD_SIGNATURE) {
        eocdOffset = offset;
        break;
      }
    }
    if (eocdOffset < 0) throw new Error('Invalid Office archive');

    const entryCount = view.getUint16(eocdOffset + 10, true);
    const centralOffset = view.getUint32(eocdOffset + 16, true);
    if (entryCount === 0xFFFF || centralOffset === 0xFFFFFFFF || entryCount > MAX_ARCHIVE_ENTRIES) {
      self.postMessage({ ok: true, safe: false } satisfies OfficeSafetyResponse);
      return;
    }

    let offset = centralOffset;
    let expandedBytes = 0;
    for (let index = 0; index < entryCount; index++) {
      if (offset + 46 > view.byteLength || view.getUint32(offset, true) !== CENTRAL_FILE_SIGNATURE) {
        throw new Error('Invalid Office archive directory');
      }
      expandedBytes += view.getUint32(offset + 24, true);
      if (expandedBytes > MAX_EXPANDED_BYTES) {
        self.postMessage({ ok: true, safe: false } satisfies OfficeSafetyResponse);
        return;
      }
      const nameLength = view.getUint16(offset + 28, true);
      const extraLength = view.getUint16(offset + 30, true);
      const commentLength = view.getUint16(offset + 32, true);
      offset += 46 + nameLength + extraLength + commentLength;
    }

    const ratio = expandedBytes / Math.max(1, event.data.blob.size);
    self.postMessage({ ok: true, safe: ratio <= MAX_COMPRESSION_RATIO } satisfies OfficeSafetyResponse);
  } catch {
    self.postMessage({ ok: true, safe: false } satisfies OfficeSafetyResponse);
  }
};
