package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestExcelVerticalMergesPreserveRecordsAndRealBlanks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.xlsx")
	book := excelize.NewFile()
	defer book.Close()
	for cell, value := range map[string]string{"A1": "category", "B1": "id", "C1": "code", "A2": "Alpha", "B2": "a", "B3": "b", "B4": "c", "C2": "001"} {
		if err := book.SetCellStr("Sheet1", cell, value); err != nil {
			t.Fatal(err)
		}
	}
	for _, cells := range [][2]string{{"A2", "A3"}, {"C2", "C3"}} {
		if err := book.MergeCell("Sheet1", cells[0], cells[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := book.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	db := newTestDuckDB(t)
	tool := &DataAnalysisTool{db: db}
	if _, err := tool.LoadFromExcel(context.Background(), path, "merged_values"); err != nil {
		t.Fatal(err)
	}
	var inherited, blank int
	if err := db.QueryRow(`SELECT count(*) FILTER (WHERE category='Alpha' AND code='001'), count(*) FILTER (WHERE category IS NULL AND code IS NULL) FROM merged_values`).Scan(&inherited, &blank); err != nil {
		t.Fatal(err)
	}
	if inherited != 2 || blank != 1 {
		t.Fatalf("merged value or genuine blank changed: inherited=%d blank=%d", inherited, blank)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("source workbook changed")
	}
}

func TestExcelVerticalMergeInHeaderDoesNotCreateRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "header.xlsx")
	book := excelize.NewFile()
	defer book.Close()
	for cell, value := range map[string]string{"A1": "category", "B1": "id", "B2": "a", "A3": "Alpha", "B3": "b"} {
		if err := book.SetCellStr("Sheet1", cell, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := book.MergeCell("Sheet1", "A1", "A2"); err != nil {
		t.Fatal(err)
	}
	if err := book.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	db := newTestDuckDB(t)
	tool := &DataAnalysisTool{db: db}
	if _, err := tool.LoadFromExcel(context.Background(), path, "header_merge"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM header_merge WHERE category IS NOT NULL`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("header merge created a data value: count=%d", count)
	}
}

func TestExcelVerticalLayoutMergeDoesNotCreateRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "layout.xlsx")
	book := excelize.NewFile()
	defer book.Close()
	for cell, value := range map[string]string{"A1": "category", "B1": "id", "A2": "第一季度", "A5": "Alpha", "B5": "a"} {
		if err := book.SetCellStr("Sheet1", cell, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := book.MergeCell("Sheet1", "A2", "A4"); err != nil {
		t.Fatal(err)
	}
	if err := book.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	db := newTestDuckDB(t)
	tool := &DataAnalysisTool{db: db}
	if _, err := tool.LoadFromExcel(context.Background(), path, "layout_merge"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM layout_merge WHERE category='第一季度'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("layout merge created records: count=%d", count)
	}
}
