package tools

import (
	"context"
	"os"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Expand only explicit vertical merges in a temporary analysis copy. Ordinary
// empty cells and horizontal layout/header merges retain their source meaning.
func prepareAnalysisExcel(ctx context.Context, filename string) (string, func(), error) {
	unchanged := func() {}
	book, err := excelize.OpenFile(filename)
	if err != nil {
		return "", unchanged, err
	}
	defer book.Close()
	changed := false
	for _, sheet := range book.GetSheetList() {
		merges, err := book.GetMergeCells(sheet)
		if err != nil {
			return "", unchanged, err
		}
		for _, merge := range merges {
			start, end := merge.GetStartAxis(), merge.GetEndAxis()
			col, first, err := excelize.CellNameToCoordinates(start)
			if err != nil {
				return "", unchanged, err
			}
			lastCol, last, err := excelize.CellNameToCoordinates(end)
			if err != nil {
				return "", unchanged, err
			}
			if col != lastCol || first == last {
				continue
			}
			value, err := book.GetCellValue(sheet, start, excelize.Options{RawCellValue: true})
			if err != nil {
				return "", unchanged, err
			}
			if value == "" {
				continue
			}
			kind, err := book.GetCellType(sheet, start)
			if err != nil {
				return "", unchanged, err
			}
			style, err := book.GetCellStyle(sheet, start)
			if err != nil {
				return "", unchanged, err
			}
			if err := book.UnmergeCell(sheet, start, end); err != nil {
				return "", unchanged, err
			}
			for row := first + 1; row <= last; row++ {
				if err := ctx.Err(); err != nil {
					return "", unchanged, err
				}
				cell, err := excelize.CoordinatesToCellName(col, row)
				if err != nil {
					return "", unchanged, err
				}
				switch kind {
				case excelize.CellTypeInlineString, excelize.CellTypeSharedString:
					err = book.SetCellStr(sheet, cell, value)
				case excelize.CellTypeBool:
					err = book.SetCellBool(sheet, cell, value == "1" || strings.EqualFold(value, "true"))
				default:
					err = book.SetCellDefault(sheet, cell, value)
				}
				if err != nil {
					return "", unchanged, err
				}
				if err := book.SetCellStyle(sheet, cell, cell, style); err != nil {
					return "", unchanged, err
				}
			}
			changed = true
		}
	}
	if !changed {
		return filename, unchanged, nil
	}
	temp, err := os.CreateTemp("", "weknora-analysis-merged-*.xlsx")
	if err != nil {
		return "", unchanged, err
	}
	path := temp.Name()
	cleanup := func() { _ = os.Remove(path) }
	if err := temp.Close(); err != nil {
		cleanup()
		return "", unchanged, err
	}
	if err := book.SaveAs(path); err != nil {
		cleanup()
		return "", unchanged, err
	}
	return path, cleanup, nil
}
