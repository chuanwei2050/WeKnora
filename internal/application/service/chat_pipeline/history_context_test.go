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

func TestPrepareMessagesDoesNotDuplicateCurrentRenderedContexts(t *testing.T) {
	manage := &types.ChatManage{}
	manage.SummaryConfig.Prompt = "system {{contexts}}"
	manage.RenderedContexts = "<context>current evidence</context>"
	manage.UserContent = "question\n<context>current evidence</context>"

	messages := prepareMessagesWithHistory(manage)
	if strings.Contains(messages[0].Content, manage.RenderedContexts) {
		t.Fatalf("current evidence duplicated in system prompt: %q", messages[0].Content)
	}
	if !strings.Contains(messages[1].Content, manage.RenderedContexts) {
		t.Fatalf("current evidence missing from user message: %q", messages[1].Content)
	}
}

func TestPrepareMessagesKeepsSystemContextsWhenUserMessageDoesNotContainThem(t *testing.T) {
	manage := &types.ChatManage{}
	manage.SummaryConfig.Prompt = "system {{contexts}}"
	manage.RenderedContexts = "<context>current evidence</context>"
	manage.UserContent = "question only"

	messages := prepareMessagesWithHistory(manage)
	if !strings.Contains(messages[0].Content, manage.RenderedContexts) {
		t.Fatalf("system prompt lost its only copy of current evidence: %q", messages[0].Content)
	}
}

func TestFormatConversationHistoryUsesOriginalQueryWithoutPersistedRAGContext(t *testing.T) {
	history := formatConversationHistory([]*types.History{{
		Query:         "question\n<context>old retrieved evidence</context>",
		OriginalQuery: "question",
		Answer:        "answer",
	}})
	if !strings.Contains(history, "User question: question\n") || !strings.Contains(history, "Assistant answer: answer") {
		t.Fatalf("conversation history lost the original exchange: %q", history)
	}
	if strings.Contains(history, "old retrieved evidence") {
		t.Fatalf("persisted RAG context leaked into query understanding history: %q", history)
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
