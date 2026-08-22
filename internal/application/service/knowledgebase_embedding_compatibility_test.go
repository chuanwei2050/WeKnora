package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestEmbeddingModelsCompatible(t *testing.T) {
	tests := []struct {
		name  string
		left  *types.Model
		right *types.Model
		want  bool
	}{
		{
			name:  "matching compatibility ID",
			left:  embeddingCompatibilityModel("online-4b", 2560, "qwen3-embedding-4b-v1"),
			right: embeddingCompatibilityModel("offline-4b", 2560, "qwen3-embedding-4b-v1"),
			want:  true,
		},
		{
			name:  "different compatibility ID",
			left:  embeddingCompatibilityModel("online-8b", 4096, "qwen3-embedding-8b-v1"),
			right: embeddingCompatibilityModel("offline-4b", 2560, "qwen3-embedding-4b-v1"),
			want:  false,
		},
		{
			name:  "same dimension but different compatibility ID",
			left:  embeddingCompatibilityModel("model-a", 2560, "space-a"),
			right: embeddingCompatibilityModel("model-b", 2560, "space-b"),
			want:  false,
		},
		{
			name:  "legacy same model ID",
			left:  embeddingCompatibilityModel("legacy", 2560, ""),
			right: embeddingCompatibilityModel("legacy", 2560, ""),
			want:  true,
		},
		{
			name:  "legacy different model ID",
			left:  embeddingCompatibilityModel("legacy-a", 2560, ""),
			right: embeddingCompatibilityModel("legacy-b", 2560, ""),
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := embeddingModelsCompatible(tt.left, tt.right); got != tt.want {
				t.Fatalf("embeddingModelsCompatible() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompatibleKnowledgeIDsFiltersVectorScopeOnly(t *testing.T) {
	active := embeddingCompatibilityModel("online-8b", 4096, "qwen3-embedding-8b-v1")
	models := []*types.Model{
		active,
		embeddingCompatibilityModel("offline-4b", 2560, "qwen3-embedding-4b-v1"),
		embeddingCompatibilityModel("other-8b", 4096, "qwen3-embedding-8b-v1"),
	}
	knowledges := []*types.Knowledge{
		{ID: "old-4b", EmbeddingModelID: "offline-4b"},
		{
			ID:                       "new-8b",
			EmbeddingModelID:         "online-8b",
			EmbeddingCompatibilityID: "qwen3-embedding-8b-v1",
			EmbeddingDimension:       4096,
		},
		{ID: "compatible-8b", EmbeddingModelID: "other-8b"},
	}

	got := compatibleKnowledgeIDs(active, models, knowledges, nil)
	if len(got) != 2 || got[0] != "new-8b" || got[1] != "compatible-8b" {
		t.Fatalf("compatibleKnowledgeIDs() = %v", got)
	}

	got = compatibleKnowledgeIDs(active, models, knowledges, []string{"old-4b", "new-8b"})
	if len(got) != 1 || got[0] != "new-8b" {
		t.Fatalf("compatibleKnowledgeIDs() with requested scope = %v", got)
	}
}

func TestKnowledgeEmbeddingCompatibilityUsesImmutableSnapshot(t *testing.T) {
	active := embeddingCompatibilityModel("edited-model", 4096, "qwen3-embedding-8b-v1")
	editedRegistration := embeddingCompatibilityModel("edited-model", 4096, "qwen3-embedding-8b-v1")
	knowledge := &types.Knowledge{
		ID:                       "historical-4b-document",
		EmbeddingModelID:         "edited-model",
		EmbeddingCompatibilityID: "qwen3-embedding-4b-v1",
		EmbeddingDimension:       2560,
	}

	if knowledgeEmbeddingCompatible(active, knowledge, editedRegistration) {
		t.Fatal("historical document was treated as compatible after its model registration changed")
	}
}

func TestCompatibleKnowledgeIDsReturnsNilWhenVectorScopeIsEmpty(t *testing.T) {
	active := embeddingCompatibilityModel("online-8b", 4096, "qwen3-embedding-8b-v1")
	models := []*types.Model{
		active,
		embeddingCompatibilityModel("offline-4b", 2560, "qwen3-embedding-4b-v1"),
	}
	knowledges := []*types.Knowledge{{ID: "old-4b", EmbeddingModelID: "offline-4b"}}

	if got := compatibleKnowledgeIDs(active, models, knowledges, nil); got != nil {
		t.Fatalf("compatibleKnowledgeIDs() = %v, want nil", got)
	}
}

func embeddingCompatibilityModel(id string, dimension int, compatibilityID string) *types.Model {
	return &types.Model{
		ID: id,
		Parameters: types.ModelParameters{
			EmbeddingParameters: types.EmbeddingParameters{
				Dimension:       dimension,
				CompatibilityID: compatibilityID,
			},
		},
	}
}
