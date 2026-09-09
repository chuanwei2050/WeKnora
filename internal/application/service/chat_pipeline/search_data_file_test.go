package chatpipeline

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestDataFileKnowledgeIDsKeepsOnlyAuthorizedReadyTables(t *testing.T) {
	items := []*types.Knowledge{
		{ID: "sheet", FileName: "人员.xlsx", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", TagID: "allowed"},
		{ID: "csv", FileName: "人员.CSV", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", TagID: "allowed"},
		{ID: "doc", FileName: "人员.docx", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", TagID: "allowed"},
		{ID: "disabled", FileName: "人员.xls", ParseStatus: types.ParseStatusCompleted, EnableStatus: "disabled", TagID: "allowed"},
		{ID: "other-tag", FileName: "人员.xlsx", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", TagID: "other"},
	}

	got := dataFileKnowledgeIDs(items, []string{"allowed"})
	if len(got) != 2 || got[0] != "sheet" || got[1] != "csv" {
		t.Fatalf("unexpected table candidates: %v", got)
	}
}
