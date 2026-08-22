declare module '@/utils/excel-preview-range.js' {
  export const EXCEL_PREVIEW_LIMITS: {
    readonly maxRows: number
    readonly maxColumns: number
    readonly maxCells: number
    readonly maxSheets: number
  }

  export function createSafeExcelPreviewSheet(
    sheet: Record<string, unknown>,
    utils: {
      decode_cell(address: string): { r: number; c: number }
      encode_range(range: { s: { r: number; c: number }; e: { r: number; c: number } }): string
    },
    limits?: typeof EXCEL_PREVIEW_LIMITS,
  ): {
    sheet: Record<string, unknown> | null
    truncated: boolean
    actualRange: { rows: number; columns: number }
  }
}
