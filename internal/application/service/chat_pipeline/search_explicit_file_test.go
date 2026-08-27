package chatpipeline

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type explicitFileKBService struct {
	interfaces.KnowledgeBaseService
	calls  int
	params types.SearchParams
	err    error
}

func (s *explicitFileKBService) HybridSearch(_ context.Context, _ string, params types.SearchParams) ([]*types.SearchResult, error) {
	s.calls++
	s.params = params
	if s.err != nil {
		return nil, s.err
	}
	return []*types.SearchResult{{ID: "hybrid", KnowledgeID: params.KnowledgeIDs[0], Score: 0.4}}, nil
}

type explicitFileChunkService struct {
	interfaces.ChunkService
	calls  int
	chunks []*types.Chunk
}

func (s *explicitFileChunkService) ListChunksByKnowledgeID(context.Context, string) ([]*types.Chunk, error) {
	s.calls++
	if s.chunks != nil {
		return s.chunks, nil
	}
	return []*types.Chunk{{ID: "direct", KnowledgeID: "knowledge", Content: "whole file", ChunkIndex: 1, ImageInfo: "[]"}}, nil
}

func (s *explicitFileChunkService) ListChunksByKnowledgeIDBounded(_ context.Context, _ uint64, _ string, maxChunks int, maxBytes int64) ([]*types.Chunk, bool, error) {
	chunks := s.chunks
	if chunks == nil {
		chunks = []*types.Chunk{{ID: "direct", KnowledgeID: "knowledge", Content: "whole file", ChunkIndex: 1, ImageInfo: "[]"}}
	}
	var bytes int64
	for _, chunk := range chunks {
		bytes += int64(len([]byte(chunk.Content)))
	}
	if len(chunks) > maxChunks || bytes > maxBytes {
		return nil, false, nil
	}
	s.calls++
	return chunks, true, nil
}

func TestExplicitFileIndexFailureUsesBoundedDirectFallback(t *testing.T) {
	kb := &explicitFileKBService{err: searchutil.IndexUnavailable(errors.New("index unavailable"))}
	chunks := &explicitFileChunkService{}
	plugin := &PluginSearch{knowledgeBaseService: kb, chunkService: chunks, knowledgeService: explicitFileKnowledgeService{}}
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{EmbeddingTopK: 5, VectorRecallTopK: 5, KeywordRecallTopK: 5},
		PipelineState:   types.PipelineState{Intent: types.IntentKBSearch},
	}
	target := &types.SearchTarget{Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "kb", KnowledgeIDs: []string{"knowledge"}}
	var mu sync.Mutex
	var results []*types.SearchResult
	plugin.searchSingleTarget(context.Background(), manage, target, "question", nil, 1, 0, nil, &mu, &results)
	if len(results) != 1 || results[0].Metadata["direct_load_reason"] != "index_unavailable" {
		t.Fatalf("index failure did not use bounded fallback: %+v", results)
	}
}

func TestDirectContextRejectsFileBeyondChunkLimit(t *testing.T) {
	chunks := make([]*types.Chunk, 51)
	for i := range chunks {
		chunks[i] = &types.Chunk{ID: string(rune('a' + i)), KnowledgeID: "knowledge", Content: "content", ImageInfo: "[]"}
	}
	chunkService := &explicitFileChunkService{chunks: chunks}
	plugin := &PluginSearch{chunkService: chunkService, knowledgeService: explicitFileKnowledgeService{}}
	results, skipped := plugin.tryDirectChunkLoading(context.Background(), 1, "kb", []string{"knowledge"}, "full_document_intent", nil)
	if len(results) != 0 || len(skipped) != 1 || skipped[0] != "knowledge" {
		t.Fatalf("oversized direct context was not rejected: results=%d skipped=%v", len(results), skipped)
	}
}

func (*explicitFileChunkService) GetRepository() interfaces.ChunkRepository { return nil }

type explicitFileKnowledgeService struct{ interfaces.KnowledgeService }

func (explicitFileKnowledgeService) GetKnowledgeBatchWithSharedAccess(context.Context, uint64, []string) ([]*types.Knowledge, error) {
	return []*types.Knowledge{{ID: "knowledge", TenantID: 1, KnowledgeBaseID: "kb", Title: "file"}}, nil
}

func TestExplicitFileNormalQuestionUsesScopedHybridSearch(t *testing.T) {
	kb := &explicitFileKBService{}
	chunks := &explicitFileChunkService{}
	plugin := &PluginSearch{knowledgeBaseService: kb, chunkService: chunks, knowledgeService: explicitFileKnowledgeService{}}
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{EmbeddingTopK: 5, VectorRecallTopK: 5, KeywordRecallTopK: 5},
		PipelineState:   types.PipelineState{Intent: types.IntentKBSearch},
	}
	target := &types.SearchTarget{Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "kb", KnowledgeIDs: []string{"knowledge"}}
	var mu sync.Mutex
	var results []*types.SearchResult

	plugin.searchSingleTarget(context.Background(), manage, target, "question", []float32{1}, 1, 0, nil, &mu, &results)
	if kb.calls != 1 || chunks.calls != 0 || len(results) != 1 {
		t.Fatalf("normal explicit-file search used wrong path: hybrid=%d direct=%d results=%+v", kb.calls, chunks.calls, results)
	}
	if len(kb.params.KnowledgeIDs) != 1 || kb.params.KnowledgeIDs[0] != "knowledge" {
		t.Fatalf("hybrid search was not scoped to the explicit file: %+v", kb.params.KnowledgeIDs)
	}
}

func TestExplicitFileNonIndexFailureDoesNotDirectLoad(t *testing.T) {
	kb := &explicitFileKBService{err: errors.New("access denied")}
	chunks := &explicitFileChunkService{}
	plugin := &PluginSearch{knowledgeBaseService: kb, chunkService: chunks, knowledgeService: explicitFileKnowledgeService{}}
	manage := &types.ChatManage{PipelineRequest: types.PipelineRequest{EmbeddingTopK: 5, VectorRecallTopK: 5, KeywordRecallTopK: 5}, PipelineState: types.PipelineState{Intent: types.IntentKBSearch}}
	target := &types.SearchTarget{Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "kb", KnowledgeIDs: []string{"knowledge"}}
	var mu sync.Mutex
	var results []*types.SearchResult

	plugin.searchSingleTarget(context.Background(), manage, target, "question", nil, 1, 0, nil, &mu, &results)
	if len(results) != 0 || chunks.calls != 0 {
		t.Fatalf("non-index failure must fail closed: direct=%d results=%+v", chunks.calls, results)
	}
}

func TestDirectContextRejectsOversizedSingleChunk(t *testing.T) {
	chunkService := &explicitFileChunkService{chunks: []*types.Chunk{{ID: "large", KnowledgeID: "knowledge", Content: string(make([]byte, maxDirectLoadBytes+1))}}}
	plugin := &PluginSearch{chunkService: chunkService, knowledgeService: explicitFileKnowledgeService{}}
	results, skipped := plugin.tryDirectChunkLoading(context.Background(), 1, "kb", []string{"knowledge"}, "full_document_intent", nil)
	if len(results) != 0 || len(skipped) != 1 || chunkService.calls != 0 {
		t.Fatalf("oversized chunk was loaded: results=%d skipped=%v calls=%d", len(results), skipped, chunkService.calls)
	}
}

func TestFullDocumentResultsPreserveOrderBeyondEmbeddingTopK(t *testing.T) {
	results := make([]*types.SearchResult, 40)
	for i := range results {
		results[i] = &types.SearchResult{ID: string(rune('a' + i)), KnowledgeID: "knowledge", ChunkIndex: i, Content: "repeated", MatchType: types.MatchTypeDirectLoad, Metadata: map[string]string{"direct_load_reason": "full_document_intent"}}
	}
	targets := []*types.SearchTarget{{Type: types.SearchTargetTypeKnowledge, KnowledgeIDs: []string{"knowledge"}}}
	got := limitRetrievalCandidates(results, 30, targets)
	if len(got) != 40 {
		t.Fatalf("full document was truncated: got=%d", len(got))
	}
	for i, result := range got {
		if result.ChunkIndex != i {
			t.Fatalf("full document order changed at %d: chunk=%d", i, result.ChunkIndex)
		}
	}
}

func TestExplicitFileSummarizeUsesBoundedDirectContextWithoutPerfectScore(t *testing.T) {
	kb := &explicitFileKBService{}
	chunks := &explicitFileChunkService{}
	plugin := &PluginSearch{knowledgeBaseService: kb, chunkService: chunks, knowledgeService: explicitFileKnowledgeService{}}
	manage := &types.ChatManage{PipelineState: types.PipelineState{Intent: types.IntentSummarize}}
	target := &types.SearchTarget{Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "kb", KnowledgeIDs: []string{"knowledge"}}
	var mu sync.Mutex
	var results []*types.SearchResult

	plugin.searchSingleTarget(context.Background(), manage, target, "summarize", nil, 1, 0, nil, &mu, &results)
	if kb.calls != 0 || chunks.calls != 1 || len(results) != 1 {
		t.Fatalf("summarize used wrong path: hybrid=%d direct=%d results=%+v", kb.calls, chunks.calls, results)
	}
	if results[0].Score == 1 || results[0].Metadata["direct_load_reason"] != "full_document_intent" {
		t.Fatalf("direct context has misleading score or reason: %+v", results[0])
	}
}

func TestExplicitFilesShareRequestDirectLoadBudget(t *testing.T) {
	kb := &explicitFileKBService{}
	chunkService := &explicitFileChunkService{chunks: explicitChunks(40)}
	plugin := &PluginSearch{knowledgeBaseService: kb, chunkService: chunkService, knowledgeService: explicitFileKnowledgeService{}}
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{EmbeddingTopK: 5, VectorRecallTopK: 5, KeywordRecallTopK: 5},
		PipelineState:   types.PipelineState{Intent: types.IntentSummarize},
	}
	target := &types.SearchTarget{Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "kb", KnowledgeIDs: []string{"knowledge"}}
	var mu sync.Mutex
	var results []*types.SearchResult

	budget := newDirectLoadBudget()
	plugin.searchSingleTarget(context.Background(), manage, target, "summarize", nil, 2, 0, budget, &mu, &results)
	chunkService.chunks = explicitChunks(20)
	plugin.searchSingleTarget(context.Background(), manage, target, "summarize", nil, 2, 1, budget, &mu, &results)

	if kb.calls != 1 {
		t.Fatalf("over-budget file was not searched: hybrid=%d", kb.calls)
	}
	if len(results) != 41 || results[len(results)-1].ID != "hybrid" {
		t.Fatalf("deterministic budget did not route the over-budget file to scoped search: count=%d tail=%+v", len(results), results[len(results)-1])
	}
}

func TestExplicitFilesReuseUnusedRequestBudget(t *testing.T) {
	kb := &explicitFileKBService{}
	chunkService := &explicitFileChunkService{chunks: explicitChunks(40)}
	plugin := &PluginSearch{knowledgeBaseService: kb, chunkService: chunkService, knowledgeService: explicitFileKnowledgeService{}}
	manage := &types.ChatManage{PipelineState: types.PipelineState{Intent: types.IntentSummarize}}
	target := &types.SearchTarget{Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "kb", KnowledgeIDs: []string{"knowledge"}}
	budget := newDirectLoadBudget()
	var mu sync.Mutex
	var results []*types.SearchResult

	plugin.searchSingleTarget(context.Background(), manage, target, "summarize", nil, 2, 0, budget, &mu, &results)
	chunkService.chunks = explicitChunks(10)
	plugin.searchSingleTarget(context.Background(), manage, target, "summarize", nil, 2, 1, budget, &mu, &results)

	if kb.calls != 0 || budget.chunks != 50 || len(results) != 50 {
		t.Fatalf("unused request budget was not reused: hybrid=%d budget=%d results=%d", kb.calls, budget.chunks, len(results))
	}
}

func explicitChunks(count int) []*types.Chunk {
	chunks := make([]*types.Chunk, count)
	for i := range chunks {
		chunks[i] = &types.Chunk{ID: string(rune('a' + i)), KnowledgeID: "knowledge", Content: "content", ChunkIndex: i, ImageInfo: "[]"}
	}
	return chunks
}

func TestDirectLoadIsExcludedFromMergeEnrichment(t *testing.T) {
	direct := &types.SearchResult{ID: "direct", MatchType: types.MatchTypeDirectLoad, ParentChunkID: "large-parent", Content: "bounded"}
	regular := &types.SearchResult{ID: "regular", MatchType: types.MatchTypeEmbedding}
	directResults, regularResults := partitionDirectLoadResults([]*types.SearchResult{direct, regular})
	if len(directResults) != 1 || directResults[0] != direct || len(regularResults) != 1 || regularResults[0] != regular {
		t.Fatalf("direct load was not isolated from merge enrichment: direct=%+v regular=%+v", directResults, regularResults)
	}
}
