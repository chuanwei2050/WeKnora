package tools

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/xuri/excelize/v2"
)

func previewAnalysisSchema(ctx context.Context, filename, fileType, tableName string) (*TableSchema, error) {
	var headers []string
	switch fileType {
	case "csv":
		file, err := os.Open(filename)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		headers, err = csv.NewReader(file).Read()
		if err != nil {
			return nil, fmt.Errorf("read CSV header: %w", err)
		}
	case "xlsx", "xls":
		workbook, err := excelize.OpenFile(filename)
		if err != nil {
			return nil, fmt.Errorf("open Excel header: %w", err)
		}
		defer workbook.Close()
		for _, sheet := range workbook.GetSheetList() {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			rows, err := workbook.Rows(sheet)
			if err != nil {
				return nil, fmt.Errorf("open sheet %q: %w", sheet, err)
			}
			if rows.Next() {
				row, rowErr := rows.Columns()
				if rowErr != nil {
					_ = rows.Close()
					return nil, fmt.Errorf("read sheet %q header: %w", sheet, rowErr)
				}
				headers = appendUniqueHeaders(headers, row...)
			}
			if err := rows.Close(); err != nil && err != io.EOF {
				return nil, fmt.Errorf("close sheet %q rows: %w", sheet, err)
			}
		}
		headers = appendUniqueHeaders(headers, excelSheetNameColumn)
	default:
		return nil, fmt.Errorf("unsupported preview file type %q", fileType)
	}
	if len(headers) == 0 {
		return nil, fmt.Errorf("table header is empty")
	}
	columns := make([]ColumnInfo, 0, len(headers))
	for _, header := range headers {
		header = strings.TrimSpace(header)
		if header != "" {
			columns = append(columns, ColumnInfo{Name: header, Type: "VARCHAR"})
		}
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("table header has no named columns")
	}
	return &TableSchema{TableName: tableName, Columns: columns}, nil
}

func appendUniqueHeaders(headers []string, additions ...string) []string {
	seen := make(map[string]bool, len(headers)+len(additions))
	for _, header := range headers {
		seen[normalizeIdentifierForMatch(header)] = true
	}
	for _, header := range additions {
		key := normalizeIdentifierForMatch(header)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		headers = append(headers, header)
	}
	return headers
}
