package chatpipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/rerank"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

func TestRerankPreservesLiteralEvidence(t *testing.T) {
	for _, body := range []string{"if x < y { return a * b }", "https://example.com", "**literal**"} {
		for _, fence := range []string{"```", "~~~"} {
			if got := cleanPassageForRerank(fence + "go\n" + body + "\n" + fence); got != body {
				t.Fatalf("literal corrupted: %q", got)
			}
		}
	}
	if got := cleanPassageForRerank("$$x < y * z$$"); got != "x < y * z" {
		t.Fatalf("formula corrupted: %q", got)
	}
}

func TestGraphEvidenceSurvivesFullOrdinaryRecall(t *testing.T) {
	var ordinary []*types.SearchResult
	for i := 0; i < 30; i++ {
		ordinary = append(ordinary, &types.SearchResult{ID: fmt.Sprint(i), Content: fmt.Sprintf("ordinary %d", i)})
	}
	graph := &types.SearchResult{ID: "graph", Content: "graph answer"}
	got := prepareRerankCandidates(interleaveSearchResults(ordinary, []*types.SearchResult{graph}), 20)
	if len(got) != 20 || got[1] != graph {
		t.Fatalf("graph evidence excluded: %+v", got)
	}
	if got := interleaveSearchResults(nil, []*types.SearchResult{graph}); len(got) != 1 || got[0] != graph {
		t.Fatal("single channel lost")
	}
}

func TestOverlapPreservesDistinctClaims(t *testing.T) {
	for _, pair := range [][2]string{
		{"production access is allowed", "production access is not allowed"},
		{"request timeout is 30 seconds", "request timeout is 60 seconds"},
		{"Access allowed for administrators", "Access allowed for everyone"},
	} {
		for _, sameSource := range []bool{false, true} {
			kb := "other"
			if sameSource {
				kb = "doc"
			}
			results := []*types.SearchResult{{ID: "a", KnowledgeID: "doc", Content: pair[0], Score: 0.9}, {ID: "b", KnowledgeID: kb, Content: pair[1], Score: 0.8}}
			if got := removePartialOverlaps(context.Background(), results); len(got) != 2 {
				t.Fatalf("distinct claim deleted: %v", pair)
			}
		}
	}
}

func TestOverlapKeepsFullEvidenceAndSourceBoundaries(t *testing.T) {
	for _, otherVersion := range []bool{false, true} {
		long := &types.SearchResult{ID: "long", KnowledgeID: "doc", Content: "prefix exact evidence suffix", Score: 0.2, StartAt: 0, EndAt: 28}
		short := &types.SearchResult{ID: "short", KnowledgeID: "doc", Content: "exact evidence", Score: 0.9, StartAt: 7, EndAt: 21}
		if otherVersion {
			short.KnowledgeVersionID = "other"
		}
		got := removePartialOverlaps(context.Background(), []*types.SearchResult{short, long})
		if otherVersion {
			if len(got) != 2 {
				t.Fatal("different versions deduplicated")
			}
		} else if len(got) != 1 || got[0] != long || long.Score != 0.9 {
			t.Fatalf("full evidence lost: %+v", got)
		}
	}
}

type auditReranker struct {
	results []rerank.RankResult
	calls   int
}

func (r *auditReranker) Rerank(context.Context, string, []string) ([]rerank.RankResult, error) {
	r.calls++
	return r.results, nil
}
func (*auditReranker) GetModelID() string   { return "audit" }
func (*auditReranker) GetModelName() string { return "audit" }

func TestRerankLowConfidenceUsesOneInference(t *testing.T) {
	model := &auditReranker{results: []rerank.RankResult{{Index: 0, RelevanceScore: 0.01}}}
	plugin := &PluginRerank{modelService: fixedRerankModelService{model: model}}
	manage := &types.ChatManage{PipelineRequest: types.PipelineRequest{RerankThreshold: 0.8, RerankTopK: 1}, PipelineState: types.PipelineState{RewriteQuery: "query", SearchResult: []*types.SearchResult{{ID: "a", Content: "evidence"}}}}
	if err := plugin.OnEvent(context.Background(), types.CHUNK_RERANK, manage, func() *PluginError { return nil }); err != ErrSearchNothing {
		t.Fatalf("expected no relevant evidence: %v", err)
	}
	if model.calls != 1 || manage.RerankThreshold != 0.8 {
		t.Fatalf("calls=%d threshold=%f", model.calls, manage.RerankThreshold)
	}
}

func TestRerankCacheRejectsInvalidResponsesAndRecovers(t *testing.T) {
	cache := &fakeRerankRedisCache{values: map[string][]byte{}}
	plugin := &PluginRerank{cache: cache}
	model := &auditReranker{results: []rerank.RankResult{{Index: 9, RelevanceScore: 0.9}}}
	candidates := []*types.SearchResult{{ID: "a"}}
	passages := []string{"evidence"}
	if _, err := plugin.rerankWithCache(context.Background(), model, "query", passages, candidates); !errors.Is(err, rerank.ErrInvalidResponse) {
		t.Fatalf("invalid response accepted: %v", err)
	}
	if len(cache.values) != 0 {
		t.Fatal("invalid response cached")
	}
	key := buildRerankCacheKey(model.GetModelID(), "query", passages, candidates)
	cache.values[key], _ = json.Marshal(model.results)
	model.results = []rerank.RankResult{{Index: 0, RelevanceScore: 0.8}}
	got, err := plugin.rerankWithCache(context.Background(), model, "query", passages, candidates)
	if err != nil || model.calls != 2 || len(got) != 1 || got[0].Index != 0 {
		t.Fatalf("poisoned cache did not recover: %v %+v", err, got)
	}
}

type historyChunkRepository struct {
	interfaces.ChunkRepository
	chunks []*types.Chunk
}

func (r historyChunkRepository) ListChunksByIDOnly(context.Context, []string) ([]*types.Chunk, error) {
	return r.chunks, nil
}

type historyChunkService struct {
	interfaces.ChunkService
	repo interfaces.ChunkRepository
}

func (s historyChunkService) GetRepository() interfaces.ChunkRepository { return s.repo }

func TestHistoryRevalidatesScopeVersionAndCurrentChunk(t *testing.T) {
	expired := time.Now().Add(-time.Hour)
	for _, scenario := range []string{"valid", "other_kb", "other_document", "disabled_tag", "empty_tags", "stale_version", "expired", "revoked", "disabled_chunk", "deleted_chunk"} {
		t.Run(scenario, func(t *testing.T) {
			historical := &types.SearchResult{ID: "chunk", KnowledgeID: "doc", KnowledgeBaseID: "kb", KnowledgeVersionID: "v1", Content: "production access", Score: 0.9, Metadata: map[string]string{"existing": "value"}}
			target := &types.SearchTarget{Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "kb", KnowledgeIDs: []string{"doc"}, TagIDs: []string{"enabled"}}
			knowledge := &types.Knowledge{ID: "doc", KnowledgeBaseID: "kb", CurrentVersionID: "v1"}
			version := &types.KnowledgeVersion{ID: "v1", Status: types.KnowledgeVersionActive}
			chunk := &types.Chunk{ID: "chunk", KnowledgeID: "doc", KnowledgeBaseID: "kb", KnowledgeVersionID: "v1", IsEnabled: true, TagID: "enabled", Content: "current production access", ChunkType: types.ChunkTypeText}
			items := []*types.Knowledge{knowledge}
			switch scenario {
			case "other_kb":
				target.KnowledgeBaseID = "other"
			case "other_document":
				target.KnowledgeIDs = []string{"other"}
			case "disabled_tag":
				chunk.TagID = "disabled"
			case "empty_tags":
				target.TagIDs = []string{}
			case "stale_version":
				knowledge.CurrentVersionID = "v2"
			case "expired":
				version.ExpiresAt = &expired
			case "revoked":
				items = nil
			case "disabled_chunk":
				chunk.IsEnabled = false
			case "deleted_chunk":
				chunk.DeletedAt.Valid = true
			}
			manage := &types.ChatManage{PipelineRequest: types.PipelineRequest{TenantID: 7, SearchTargets: types.SearchTargets{target}}, PipelineState: types.PipelineState{RewriteQuery: "production access", History: []*types.History{{KnowledgeReferences: []*types.SearchResult{historical}}}}}
			plugin := &PluginSearch{knowledgeService: governanceKnowledgeFixture{items: items}, governanceRepo: governanceVersionFixture{versions: map[string]*types.KnowledgeVersion{"v1": version}}, chunkService: historyChunkService{repo: historyChunkRepository{chunks: []*types.Chunk{chunk}}}}
			got := plugin.searchHistory(context.Background(), manage, nil)
			if scenario == "valid" {
				if len(got) != 1 || got[0].Content != chunk.Content {
					t.Fatalf("valid history not refreshed: %+v", got)
				}
			} else if len(got) != 0 {
				t.Fatalf("invalid history admitted: %+v", got)
			}
			if historical.Score != 0.9 || historical.Content != "production access" || len(historical.Metadata) != 1 {
				t.Fatal("persisted history mutated")
			}
		})
	}
}

func TestHistoryWithoutExplicitTargetsKeepsAuthorizedCurrentChunk(t *testing.T) {
	historical := &types.SearchResult{ID: "chunk", KnowledgeID: "doc", KnowledgeBaseID: "kb", KnowledgeVersionID: "v1", Content: "production access", Score: 0.9}
	knowledge := &types.Knowledge{ID: "doc", KnowledgeBaseID: "kb", CurrentVersionID: "v1"}
	version := &types.KnowledgeVersion{ID: "v1", Status: types.KnowledgeVersionActive}
	chunk := &types.Chunk{ID: "chunk", KnowledgeID: "doc", KnowledgeBaseID: "kb", KnowledgeVersionID: "v1", IsEnabled: true, Content: "current production access", ChunkType: types.ChunkTypeText}
	manage := &types.ChatManage{PipelineRequest: types.PipelineRequest{TenantID: 7}, PipelineState: types.PipelineState{RewriteQuery: "production access", History: []*types.History{{KnowledgeReferences: []*types.SearchResult{historical}}}}}
	plugin := &PluginSearch{knowledgeService: governanceKnowledgeFixture{items: []*types.Knowledge{knowledge}}, governanceRepo: governanceVersionFixture{versions: map[string]*types.KnowledgeVersion{"v1": version}}, chunkService: historyChunkService{repo: historyChunkRepository{chunks: []*types.Chunk{chunk}}}}

	got := plugin.searchHistory(context.Background(), manage, nil)
	if len(got) != 1 || got[0].Content != chunk.Content {
		t.Fatalf("authorized history was removed without explicit targets: %+v", got)
	}
}

func TestMergeCannotReintroduceRejectedHistory(t *testing.T) {
	manage := &types.ChatManage{PipelineState: types.PipelineState{RewriteQuery: "production access", History: []*types.History{{KnowledgeReferences: []*types.SearchResult{{ID: "old", Content: "production access", Score: 1}}}}}}
	err := (&PluginMerge{}).OnEvent(context.Background(), types.CHUNK_MERGE, manage, func() *PluginError { return nil })
	if err != nil || len(manage.MergeResult) != 0 {
		t.Fatalf("history bypassed retrieval and rerank: %v %+v", err, manage.MergeResult)
	}
}

func TestRerankUnsortedResponseUsesActualBestFallback(t *testing.T) {
	model := &auditReranker{results: []rerank.RankResult{{Index: 0, RelevanceScore: 0.01}, {Index: 1, RelevanceScore: 0.4}}}
	plugin := &PluginRerank{}
	candidates := []*types.SearchResult{{ID: "low"}, {ID: "best"}}
	manage := &types.ChatManage{PipelineRequest: types.PipelineRequest{RerankThreshold: 0.8}}
	got, err := plugin.rerank(context.Background(), manage, model, "query", []string{"low", "best"}, candidates)
	if err != nil || len(got) != 1 || got[0].Index != 1 || model.calls != 1 {
		t.Fatalf("best fallback lost: %v %+v", err, got)
	}
}

func TestOverlapDoesNotMergeSeparateOccurrences(t *testing.T) {
	results := []*types.SearchResult{
		{ID: "positive", KnowledgeID: "doc", Content: "access allowed", StartAt: 0, EndAt: 14},
		{ID: "conditional", KnowledgeID: "doc", Content: "access allowed only with approval", StartAt: 100, EndAt: 133},
	}
	if got := removePartialOverlaps(context.Background(), results); len(got) != 2 {
		t.Fatal("separate claims merged")
	}
}
