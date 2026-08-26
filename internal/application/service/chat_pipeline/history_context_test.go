package chatpipeline

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestPrepareMessagesUsesOriginalHistoryQueryWithoutPersistedRAGContext(t *testing.T) {
	manage := &types.ChatManage{
		PipelineState: types.PipelineState{
			History: []*types.History{{
				Query:         "question\n<context>old retrieved evidence</context>",
				OriginalQuery: "question",
				Answer:        "answer",
			}},
		},
	}
	manage.SummaryConfig.Prompt = "system"
	manage.UserContent = "current question with current evidence"

	messages := prepareMessagesWithHistory(manage)
	if len(messages) != 4 {
		t.Fatalf("unexpected message count: %d", len(messages))
	}
	if messages[1].Content != "question" {
		t.Fatalf("persisted RAG context leaked into final history: %q", messages[1].Content)
	}
	if messages[2].Content != "answer" {
		t.Fatalf("assistant history must remain available: %q", messages[2].Content)
	}
}

func TestHistoricalUserContentKeepsNonRAGContext(t *testing.T) {
	message := &types.Message{
		Content: "question",
		RenderedContent: "question\n<context>old retrieved evidence</context>\n" +
			"<quoted_message>previous answer</quoted_message>",
		Images: types.MessageImages{{Caption: "image description"}},
		Attachments: types.MessageAttachments{{
			FileName: "notes.txt",
			FileType: ".txt",
			Content:  "attachment body",
		}},
	}

	content := historicalUserContent(message)
	for _, expected := range []string{
		"question",
		"image description",
		"<quoted_message>previous answer</quoted_message>",
		"attachment body",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("historical context omitted %q: %q", expected, content)
		}
	}
	if strings.Contains(content, "old retrieved evidence") {
		t.Fatalf("persisted RAG evidence leaked into history: %q", content)
	}
}
