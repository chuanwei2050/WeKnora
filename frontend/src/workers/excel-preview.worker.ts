import * as XLSX from 'xlsx';
import { createSafeExcelPreviewSheet, EXCEL_PREVIEW_LIMITS } from '@/utils/excel-preview-range.js';

type ExcelPreviewRequest = {
  buffer: ArrayBuffer;
  fileType: string;
};

type ExcelPreviewResponse =
  | { ok: true; html: string; truncated: boolean }
  | { ok: false; message: string };

function isValidUTF8(bytes: Uint8Array): boolean {
  for (let i = 0; i < bytes.length;) {
    const byte = bytes[i];
    let remaining = 0;
    if (byte <= 0x7F) remaining = 0;
    else if ((byte & 0xE0) === 0xC0) remaining = 1;
    else if ((byte & 0xF0) === 0xE0) remaining = 2;
    else if ((byte & 0xF8) === 0xF0) remaining = 3;
    else return false;
    if (i + remaining >= bytes.length) return false;
    for (let offset = 1; offset <= remaining; offset++) {
      if ((bytes[i + offset] & 0xC0) !== 0x80) return false;
    }
    i += 1 + remaining;
  }
  return true;
}

function decodeCSV(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  if (bytes[0] === 0xEF && bytes[1] === 0xBB && bytes[2] === 0xBF) {
    return new TextDecoder('utf-8').decode(bytes);
  }
  return new TextDecoder(isValidUTF8(bytes) ? 'utf-8' : 'gbk').decode(bytes);
}

self.onmessage = (event: MessageEvent<ExcelPreviewRequest>) => {
  try {
    const { buffer, fileType } = event.data;
    const readOptions = { sheetRows: EXCEL_PREVIEW_LIMITS.maxRows + 1 };
    const workbook = fileType.toLowerCase() === 'csv'
      ? XLSX.read(decodeCSV(buffer), { ...readOptions, type: 'string' })
      : XLSX.read(buffer, { ...readOptions, type: 'array' });

    let html = '';
    const previewSheetNames = workbook.SheetNames.slice(0, EXCEL_PREVIEW_LIMITS.maxSheets);
    let truncated = previewSheetNames.length < workbook.SheetNames.length;
    previewSheetNames.forEach((name, sheetIndex) => {
      const sourceSheet = workbook.Sheets[name];
      const result = createSafeExcelPreviewSheet(sourceSheet, XLSX.utils);
      if (!result.sheet) return;
      if (result.truncated || sourceSheet['!fullref']) truncated = true;
      html += '<div class="excel-sheet">';
      if (workbook.SheetNames.length > 1) {
        html += `<div class="excel-sheet-name">${name}</div>`;
      }
      html += XLSX.utils.sheet_to_html(result.sheet, { id: `sheet-${sheetIndex}` });
      html += '</div>';
    });

    self.postMessage({ ok: true, html, truncated } satisfies ExcelPreviewResponse);
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    self.postMessage({ ok: false, message } satisfies ExcelPreviewResponse);
  }
};
