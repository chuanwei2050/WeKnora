package tools

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestPreviewAnalysisSchemaReadsHeadersAcrossSheets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preview.xlsx")
	workbook := excelize.NewFile()
	workbook.SetSheetName("Sheet1", "人员")
	if err := workbook.SetSheetRow("人员", "A1", &[]string{"姓名", "专业证书"}); err != nil {
		t.Fatal(err)
	}
	if _, err := workbook.NewSheet("部门"); err != nil {
		t.Fatal(err)
	}
	if err := workbook.SetSheetRow("部门", "A1", &[]string{"部门", "姓名"}); err != nil {
		t.Fatal(err)
	}
	if err := workbook.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	_ = workbook.Close()

	schema, err := previewAnalysisSchema(context.Background(), path, "xlsx", "data")
	if err != nil {
		t.Fatalf("preview schema: %v", err)
	}
	got := make(map[string]bool)
	for _, column := range schema.Columns {
		got[column.Name] = true
	}
	for _, name := range []string{"姓名", "专业证书", "部门", excelSheetNameColumn} {
		if !got[name] {
			t.Fatalf("preview missing %q: %#v", name, schema.Columns)
		}
	}
}
