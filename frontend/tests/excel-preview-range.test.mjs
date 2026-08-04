import assert from 'node:assert/strict';
import test from 'node:test';
import * as XLSX from 'xlsx';

import {
  EXCEL_PREVIEW_LIMITS,
  createSafeExcelPreviewSheet,
} from '../src/utils/excel-preview-range.js';

test('shrinks a stale worksheet dimension to populated cells', () => {
  const sheet = {
    A1: { t: 's', v: '名称' },
    K72: { t: 'n', v: 72 },
    '!ref': 'A1:K1048249',
  };

  const result = createSafeExcelPreviewSheet(sheet, XLSX.utils);

  assert.equal(result.sheet['!ref'], 'A1:K72');
  assert.equal(result.truncated, false);
  assert.deepEqual(result.actualRange, { rows: 72, columns: 11 });
});

test('caps genuinely large populated worksheets', () => {
  const sheet = {
    A1: { t: 's', v: '开始' },
    CV2000: { t: 's', v: '结束' },
    '!ref': 'A1:CV2000',
  };

  const result = createSafeExcelPreviewSheet(sheet, XLSX.utils);
  const range = XLSX.utils.decode_range(result.sheet['!ref']);
  const rows = range.e.r - range.s.r + 1;
  const columns = range.e.c - range.s.c + 1;

  assert.equal(result.truncated, true);
  assert.ok(rows <= EXCEL_PREVIEW_LIMITS.maxRows);
  assert.ok(columns <= EXCEL_PREVIEW_LIMITS.maxColumns);
  assert.ok(rows * columns <= EXCEL_PREVIEW_LIMITS.maxCells);
});

test('returns no preview sheet for an empty worksheet', () => {
  const result = createSafeExcelPreviewSheet({ '!ref': 'A1:XFD1048576' }, XLSX.utils);

  assert.equal(result.sheet, null);
  assert.equal(result.truncated, false);
  assert.deepEqual(result.actualRange, { rows: 0, columns: 0 });
});
