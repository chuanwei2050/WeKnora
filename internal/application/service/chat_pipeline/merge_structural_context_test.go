package chatpipeline

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestInferStructuralContextAcrossPredecessorChunks(t *testing.T) {
	ancestor := &types.Chunk{ID: "ancestor", KnowledgeID: "knowledge", KnowledgeVersionID: "version", Content: "## 主体甲\n内容"}
	previous := &types.Chunk{ID: "previous", PreChunkID: ancestor.ID, KnowledgeID: "knowledge", KnowledgeVersionID: "version", Content: "表格续页\n\n### 附属材料"}
	current := &types.Chunk{ID: "current", PreChunkID: previous.ID, KnowledgeID: "knowledge", KnowledgeVersionID: "version", Content: "### 附属材料\n\n#### 材料类别\n图片"}

	got := inferStructuralContext(current, map[string]*types.Chunk{
		ancestor.ID: ancestor, previous.ID: previous, current.ID: current,
	})
	if got != "主体甲" {
		t.Fatalf("structural context = %q, want 主体甲", got)
	}
}

func TestInferStructuralContextRejectsCrossingPeerSection(t *testing.T) {
	previous := &types.Chunk{ID: "previous", KnowledgeID: "knowledge", KnowledgeVersionID: "version", Content: "## 主体甲\n内容"}
	current := &types.Chunk{ID: "current", PreChunkID: previous.ID, KnowledgeID: "knowledge", KnowledgeVersionID: "version", Content: "### 附属材料\n材料\n\n## 主体乙\n内容"}

	if got := inferStructuralContext(current, map[string]*types.Chunk{previous.ID: previous}); got != "" {
		t.Fatalf("cross-section context must be empty, got %q", got)
	}
}
