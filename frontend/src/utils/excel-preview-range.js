export const EXCEL_PREVIEW_LIMITS = Object.freeze({
  maxRows: 1000,
  maxColumns: 50,
  maxCells: 50000,
  maxSheets: 10,
});

const CELL_ADDRESS_PATTERN = /^[A-Z]+[1-9]\d*$/i;

const hasRenderableValue = (cell) => (
  cell
  && ((cell.v !== undefined && cell.v !== null && cell.v !== '')
    || (typeof cell.f === 'string' && cell.f.length > 0))
);

export function createSafeExcelPreviewSheet(sheet, utils, limits = EXCEL_PREVIEW_LIMITS) {
  let minRow = Number.POSITIVE_INFINITY;
  let minColumn = Number.POSITIVE_INFINITY;
  let maxRow = -1;
  let maxColumn = -1;

  Object.keys(sheet || {}).forEach((address) => {
    if (!CELL_ADDRESS_PATTERN.test(address) || !hasRenderableValue(sheet[address])) return;
    const cell = utils.decode_cell(address);
    minRow = Math.min(minRow, cell.r);
    minColumn = Math.min(minColumn, cell.c);
    maxRow = Math.max(maxRow, cell.r);
    maxColumn = Math.max(maxColumn, cell.c);
  });

  if (maxRow < 0 || maxColumn < 0) {
    return {
      sheet: null,
      truncated: false,
      actualRange: { rows: 0, columns: 0 },
    };
  }

  const actualRows = maxRow - minRow + 1;
  const actualColumns = maxColumn - minColumn + 1;
  const previewColumns = Math.min(actualColumns, limits.maxColumns);
  const previewRows = Math.min(
    actualRows,
    limits.maxRows,
    Math.max(1, Math.floor(limits.maxCells / previewColumns)),
  );
  const previewEnd = {
    r: minRow + previewRows - 1,
    c: minColumn + previewColumns - 1,
  };
  const previewRange = { s: { r: minRow, c: minColumn }, e: previewEnd };

  return {
    sheet: {
      ...sheet,
      '!ref': utils.encode_range(previewRange),
      ...(Array.isArray(sheet['!merges'])
        ? {
            '!merges': sheet['!merges'].filter((merge) => (
              merge.s.r <= previewEnd.r
              && merge.s.c <= previewEnd.c
              && merge.e.r >= minRow
              && merge.e.c >= minColumn
            )),
          }
        : {}),
    },
    truncated: previewRows < actualRows || previewColumns < actualColumns,
    actualRange: { rows: actualRows, columns: actualColumns },
  };
}
