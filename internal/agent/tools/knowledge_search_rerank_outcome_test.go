package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/rerank"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type rerankOutcomeKBService struct {
	interfaces.KnowledgeBaseService
	result *types.SearchResult
}

type rerankOutcomeKnowledgeService struct {
	interfaces.KnowledgeService
	items []*types.Knowledge
}

func (s rerankOutcomeKnowledgeService) GetKnowledgeBatchWithSharedAccess(context.Context, uint64, []string) ([]*types.Knowledge, error) {
	return s.items, nil
}

func (s rerankOutcomeKBService) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{ID: "kb", Type: types.KnowledgeBaseTypeDocument, IndexingStrategy: types.DefaultIndexingStrategy()}, nil
}

func (s rerankOutcomeKBService) GetKnowledgeBasesByIDsOnly(context.Context, []string) ([]*types.KnowledgeBase, error) {
	return []*types.KnowledgeBase{{ID: "kb", Type: types.KnowledgeBaseTypeDocument, IndexingStrategy: types.DefaultIndexingStrategy()}}, nil
}

func (rerankOutcomeKBService) ResolveEmbeddingModelKeys(context.Context, []*types.KnowledgeBase) map[string]string {
	return map[string]string{"kb": ""}
}

func (s rerankOutcomeKBService) HybridSearch(context.Context, string, types.SearchParams) ([]*types.SearchResult, error) {
	return []*types.SearchResult{s.result}, nil
}

type outcomeReranker struct {
	results []rerank.RankResult
	err     error
}

func (r outcomeReranker) Rerank(context.Context, string, []string) ([]rerank.RankResult, error) {
	return r.results, r.err
}

func TestAgentRerankInvalidResponseFailsClosed(t *testing.T) {
	tool := &KnowledgeSearchTool{rerankModel: outcomeReranker{results: []rerank.RankResult{
		{Index: 0, RelevanceScore: 0.9},
		{Index: -1, RelevanceScore: 0.8},
	}}}
	input := []*searchResultWithMeta{{SearchResult: &types.SearchResult{ID: "chunk", Content: "content"}}}

	got, err := tool.rerankResults(context.Background(), "query", input)
	if !errors.Is(err, rerank.ErrInvalidResponse) || got != nil {
		t.Fatalf("invalid rerank response must fail closed: results=%+v err=%v", got, err)
	}
}

func TestAgentExecuteInvalidRerankResponseDoesNotRestoreRawRecall(t *testing.T) {
	candidate := &types.SearchResult{ID: "chunk", KnowledgeID: "knowledge", KnowledgeBaseID: "kb", Content: "content"}
	tool := &KnowledgeSearchTool{
		knowledgeBaseService: rerankOutcomeKBService{result: candidate},
		knowledgeService: rerankOutcomeKnowledgeService{items: []*types.Knowledge{
			{ID: "knowledge", TenantID: 9},
		}},
		searchTargets: types.SearchTargets{{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb", TenantID: 9}},
		rerankModel: outcomeReranker{results: []rerank.RankResult{
			{Index: 0, RelevanceScore: 0.9},
			{Index: -1, RelevanceScore: 0.8},
		}},
	}

	got, err := tool.Execute(context.Background(), json.RawMessage(`{"queries":["query"]}`))
	if !errors.Is(err, rerank.ErrInvalidResponse) || got == nil || got.Success || got.Output != "" {
		t.Fatalf("invalid rerank response leaked raw recall: result=%+v err=%v", got, err)
	}
}
func (outcomeReranker) GetModelName() string { return "outcome" }
func (outcomeReranker) GetModelID() string   { return "outcome" }

func TestAgentRerankEmptyDoesNotRestoreRawRecall(t *testing.T) {
	tool := &KnowledgeSearchTool{rerankModel: outcomeReranker{}}
	input := []*searchResultWithMeta{{SearchResult: &types.SearchResult{ID: "chunk", Content: "content"}}}
	got, err := tool.rerankResults(context.Background(), "query", input)
	if err != nil || len(got) != 0 {
		t.Fatalf("successful empty rerank should remain empty: results=%+v err=%v", got, err)
	}
}

func TestAgentRerankUnavailableRestoresRawRecall(t *testing.T) {
	tool := &KnowledgeSearchTool{rerankModel: outcomeReranker{err: errors.New("unavailable")}}
	input := []*searchResultWithMeta{{SearchResult: &types.SearchResult{ID: "chunk", Content: "content"}}}
	got, err := tool.rerankResults(context.Background(), "query", input)
	if err != nil || len(got) != 1 || got[0].ID != "chunk" {
		t.Fatalf("unavailable rerank should restore raw recall: results=%+v err=%v", got, err)
	}
}

func TestAgentLLMRerankScoresRequireExactFiniteRange(t *testing.T) {
	tool := &KnowledgeSearchTool{}
	for name, response := range map[string]string{
		"missing":      "Passage 1: 0.8",
		"extra":        "Passage 1: 0.8\nPassage 2: 0.7\nPassage 3: 0.6",
		"out of range": "Passage 1: 1.2\nPassage 2: 0.7",
		"not numeric":  "Passage 1: good\nPassage 2: 0.7",
		"reordered":    "Passage 2: 0.8\nPassage 1: 0.7",
		"duplicate":    "Passage 1: 0.8\nPassage 1: 0.7",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := tool.parseScoresFromResponse(response, 2); err == nil {
				t.Fatalf("malformed LLM rerank response %q was accepted", response)
			}
		})
	}
}
