package service

import (
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestPreserveRetrieverLeadersKeepsKeywordOnlyExactMatch(t *testing.T) {
	vectorResults := make([]*types.IndexWithScore, 30)
	keywordResults := make([]*types.IndexWithScore, 30)
	for i := range 30 {
		vectorResults[i] = &types.IndexWithScore{ChunkID: fmt.Sprintf("vector-%02d", i)}
		keywordResults[i] = &types.IndexWithScore{ChunkID: fmt.Sprintf("keyword-%02d", i)}
	}
	target := keywordResults[2]

	// Model the failure mode: RRF's first 30 slots are occupied by chunks that
	// appear in both channels, while the exact keyword-only hit is ranked third
	// by Elasticsearch but falls outside the fused cutoff.
	fused := make([]*types.IndexWithScore, 31)
	for i := range 30 {
		fused[i] = &types.IndexWithScore{ChunkID: fmt.Sprintf("fused-%02d", i)}
	}
	fused[30] = target

	got := preserveRetrieverLeaders(fused, vectorResults, keywordResults, 20, 30)
	if len(got) != 30 {
		t.Fatalf("expected 30 candidates, got %d", len(got))
	}
	for i, candidate := range got[:25] {
		if candidate.ChunkID == target.ChunkID {
			return
		}
		t.Logf("candidate %d: %s", i, candidate.ChunkID)
	}
	t.Fatalf("keyword rank-3 candidate %q was excluded from the rerank pool", target.ChunkID)
}

func TestPreserveRetrieverLeadersUsesRerankCandidateBudget(t *testing.T) {
	vectorResults := make([]*types.IndexWithScore, 30)
	keywordResults := make([]*types.IndexWithScore, 30)
	for i := range 30 {
		vectorResults[i] = &types.IndexWithScore{ChunkID: fmt.Sprintf("vector-%02d", i)}
		keywordResults[i] = &types.IndexWithScore{ChunkID: fmt.Sprintf("keyword-%02d", i)}
	}

	got := preserveRetrieverLeaders(
		append(append([]*types.IndexWithScore{}, vectorResults...), keywordResults...),
		vectorResults,
		keywordResults,
		20,
		30,
	)

	seen := make(map[string]struct{}, 20)
	for _, candidate := range got[:20] {
		seen[candidate.ChunkID] = struct{}{}
	}
	for _, expected := range []string{"vector-09", "keyword-09"} {
		if _, ok := seen[expected]; !ok {
			t.Fatalf("retriever leader %q was excluded from the downstream rerank budget", expected)
		}
	}
}

func TestMarkKeywordLeaderUsesPerSearchRankInsteadOfRawScore(t *testing.T) {
	results := []*types.SearchResult{{ID: "local-first"}, {ID: "local-second"}}
	keyword := []*types.IndexWithScore{
		{ChunkID: "local-first", Score: 2},
		{ChunkID: "local-second", Score: 100},
	}

	markKeywordLeader(results, keyword)
	if results[0].Metadata["keyword_leader"] != "true" {
		t.Fatal("expected the retrieval-ranked leader to be marked")
	}
	if results[1].Metadata["keyword_leader"] == "true" {
		t.Fatal("raw scores must not redefine the per-search leader")
	}
}

func TestPreserveRetrieverLeadersProtectsCandidateWindowBeforeResultLimit(t *testing.T) {
	vectorResults := make([]*types.IndexWithScore, 25)
	keywordResults := make([]*types.IndexWithScore, 25)
	for i := range 25 {
		vectorResults[i] = &types.IndexWithScore{ChunkID: fmt.Sprintf("vector-%02d", i)}
		keywordResults[i] = &types.IndexWithScore{ChunkID: fmt.Sprintf("keyword-%02d", i)}
	}
	fused := append([]*types.IndexWithScore(nil), vectorResults...)

	got := preserveRetrieverLeaders(fused, vectorResults, keywordResults, 20, 30)
	if len(got) != 30 {
		t.Fatalf("expected result limit 30, got %d", len(got))
	}
	firstTwenty := make(map[string]bool, 20)
	for _, result := range got[:20] {
		firstTwenty[result.ChunkID] = true
	}
	if !firstTwenty["keyword-00"] {
		t.Fatal("keyword leader must be inside the rerank candidate window")
	}
}

func TestPreserveRetrieverLeadersDeduplicatesSharedLeaders(t *testing.T) {
	shared := &types.IndexWithScore{ChunkID: "shared"}
	vectorResults := []*types.IndexWithScore{shared, {ChunkID: "vector"}}
	keywordResults := []*types.IndexWithScore{shared, {ChunkID: "keyword"}}
	fused := []*types.IndexWithScore{
		shared,
		{ChunkID: "vector"},
		{ChunkID: "keyword"},
		{ChunkID: "tail-1"},
		{ChunkID: "tail-2"},
	}

	got := preserveRetrieverLeaders(fused, vectorResults, keywordResults, 4, 4)
	seen := make(map[string]struct{}, len(got))
	for _, candidate := range got {
		if _, exists := seen[candidate.ChunkID]; exists {
			t.Fatalf("duplicate candidate %q", candidate.ChunkID)
		}
		seen[candidate.ChunkID] = struct{}{}
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 candidates, got %d", len(got))
	}
}

func TestFuseWithRRFDoesNotOverwriteRetrieverScores(t *testing.T) {
	vector := &types.IndexWithScore{ChunkID: "shared", Score: 0.8}
	keyword := &types.IndexWithScore{ChunkID: "keyword-only", Score: 19.3}

	fused := fuseWithRRF(t.Context(), []*types.IndexWithScore{vector}, []*types.IndexWithScore{keyword}, 0.7)
	if len(fused) != 2 {
		t.Fatalf("expected 2 fused candidates, got %d", len(fused))
	}
	if vector.Score != 0.8 {
		t.Fatalf("vector score was overwritten with %f", vector.Score)
	}
	if keyword.Score != 19.3 {
		t.Fatalf("keyword score was overwritten with %f", keyword.Score)
	}
	for _, candidate := range fused {
		if candidate.ScoreDomain != types.RetrievalScoreDomainRRF {
			t.Fatalf("fused candidate has untrusted score domain: %+v", candidate)
		}
	}
}

func TestDeduplicateMarksRelevanceScoreDomain(t *testing.T) {
	result := deduplicateByScore([]*types.IndexWithScore{{ChunkID: "vector", Score: 0.8}})
	if len(result) != 1 || result[0].ScoreDomain != types.RetrievalScoreDomainRelevance {
		t.Fatalf("single-channel candidate has wrong score domain: %+v", result)
	}
}

func TestFuseWithRRFUsesConfiguredVectorWeight(t *testing.T) {
	vector := &types.IndexWithScore{ChunkID: "vector"}
	keyword := &types.IndexWithScore{ChunkID: "keyword"}

	vectorFirst := fuseWithRRF(
		t.Context(), []*types.IndexWithScore{vector}, []*types.IndexWithScore{keyword}, 0.8,
	)
	if vectorFirst[0].ChunkID != vector.ChunkID {
		t.Fatalf("vector-heavy fusion ranked %q first", vectorFirst[0].ChunkID)
	}

	keywordFirst := fuseWithRRF(
		t.Context(), []*types.IndexWithScore{vector}, []*types.IndexWithScore{keyword}, 0.2,
	)
	if keywordFirst[0].ChunkID != keyword.ChunkID {
		t.Fatalf("keyword-heavy fusion ranked %q first", keywordFirst[0].ChunkID)
	}
}
