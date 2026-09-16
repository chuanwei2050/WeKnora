package service

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestBuildIngestChunksKeepsExcelRowsWithoutParentChild(t *testing.T) {
	longCert := strings.Repeat("系统集成项目管理工程师证书详情", 40)
	row1 := "序号: 1,姓名: 夏雨欣,专业证书: 系统集成项目管理师"
	row2 := "序号: 2,姓名: 许乃汉,专业证书: " + longCert
	markdown := row1 + "\n" + row2 + "\n"
	kb := &types.KnowledgeBase{
		ChunkingConfig: types.ChunkingConfig{
			EnableParentChild: true,
			ParentChunkSize:   4096,
			ChildChunkSize:    384,
			ChunkSize:         512,
			Separators:        []string{"\n\n", "\n", "。"},
		},
	}

	parsed, parents := buildIngestChunks("xlsx", markdown, kb)
	if len(parents) != 0 {
		t.Fatalf("excel ingest must not create parent chunks, got %d", len(parents))
	}
	if len(parsed) != 2 {
		t.Fatalf("got %d chunks, want 2 row chunks", len(parsed))
	}
	if parsed[0].Content != row1 || parsed[1].Content != row2 {
		t.Fatalf("excel rows were re-split:\n0=%q\n1=%q", parsed[0].Content, parsed[1].Content)
	}
	if utf8.RuneCountInString(parsed[1].Content) < 384 {
		t.Fatal("long excel row must stay intact")
	}
}

func TestBuildIngestChunksStillUsesParentChildForNormalDocs(t *testing.T) {
	kb := &types.KnowledgeBase{
		ChunkingConfig: types.ChunkingConfig{
			EnableParentChild: true,
			ParentChunkSize:   120,
			ChildChunkSize:    40,
			ChunkSize:         40,
			Separators:        []string{"\n\n", "\n", "。"},
		},
	}
	markdown := strings.Repeat("这是一段普通文档内容用于父子分块。", 30)
	parsed, _ := buildIngestChunks("docx", markdown, kb)
	if len(parsed) < 2 {
		t.Fatalf("expected parent-child to produce multiple children for prose, got %d", len(parsed))
	}
	for _, c := range parsed {
		if c.Content == markdown {
			t.Fatal("prose should be split when parent-child is enabled")
		}
	}
}
