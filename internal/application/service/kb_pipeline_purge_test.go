package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestIsRebuildPipelineTaskType(t *testing.T) {
	if !isRebuildPipelineTaskType(types.TypeDocumentProcess) {
		t.Fatal("document:process should match")
	}
	if !isRebuildPipelineTaskType(types.TypeImageMultimodal) {
		t.Fatal("image:multimodal should match")
	}
	if !isRebuildPipelineTaskType(types.TypeKnowledgePostProcess) {
		t.Fatal("knowledge:post_process should match")
	}
	if isRebuildPipelineTaskType(types.TypeWikiIngest) {
		t.Fatal("wiki ingest must not be purged with rebuild stop")
	}
	if isRebuildPipelineTaskType(types.TypeDataSourceSync) {
		t.Fatal("data source sync must not be purged with rebuild stop")
	}
}

func TestMultimodalPendingKeyPatterns(t *testing.T) {
	// Purge clears multimodal:pending:{id} and multimodal:pending:{id}:* via
	// multimodalPendingKey + Scan — assert the key helper stays aligned.
	if got := multimodalPendingKey("abc", ""); got != "multimodal:pending:abc" {
		t.Fatalf("base key=%q", got)
	}
	if got := multimodalPendingKey("abc", "v1"); got != "multimodal:pending:abc:v1" {
		t.Fatalf("versioned key=%q", got)
	}
}

func TestPayloadKnowledgeID(t *testing.T) {
	got := payloadKnowledgeID([]byte(`{"tenant_id":1,"knowledge_id":"abc-123","file_path":"x"}`))
	if got != "abc-123" {
		t.Fatalf("got %q", got)
	}
	if payloadKnowledgeID([]byte(`not-json`)) != "" {
		t.Fatal("invalid payload should yield empty id")
	}
}

func TestKnowledgeIDSetSkipsEmpty(t *testing.T) {
	set := knowledgeIDSet([]string{"a", "", "b", "a"})
	if len(set) != 2 {
		t.Fatalf("len=%d", len(set))
	}
}
