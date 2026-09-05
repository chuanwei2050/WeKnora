package chatpipeline

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestSelectDataAnalysisTargetPrefersExplicitDocument(t *testing.T) {
	ranked := &types.SearchResult{KnowledgeID: "similar", KnowledgeFilename: "similar.xlsx"}
	explicit := &types.SearchResult{KnowledgeID: "requested", KnowledgeFilename: "requested.xlsx"}
	targets := types.SearchTargets{{Type: types.SearchTargetTypeKnowledge, KnowledgeIDs: []string{"requested"}}}

	if got := selectDataAnalysisTarget([]*types.SearchResult{ranked, explicit}, nil, targets); got != explicit {
		t.Fatalf("expected explicitly scoped document, got %#v", got)
	}
}

func TestSelectDataAnalysisTargetUsesRankingForKnowledgeBaseScope(t *testing.T) {
	first := &types.SearchResult{KnowledgeID: "first", KnowledgeFilename: "first.xlsx"}
	second := &types.SearchResult{KnowledgeID: "second", KnowledgeFilename: "second.xlsx"}

	if got := selectDataAnalysisTarget([]*types.SearchResult{first, second}, nil, nil); got != first {
		t.Fatalf("expected highest-ranked table, got %#v", got)
	}
}

func TestSelectDataAnalysisTargetsUsesThreeDistinctRankedTables(t *testing.T) {
	firstChunk := &types.SearchResult{ID: "a1", KnowledgeID: "first", KnowledgeFilename: "first.xlsx"}
	duplicateFirst := &types.SearchResult{ID: "a2", KnowledgeID: "first", KnowledgeFilename: "first.xlsx"}
	duplicateUpload := &types.SearchResult{ID: "a3", KnowledgeID: "first-copy", KnowledgeFilename: "first.xlsx"}
	second := &types.SearchResult{KnowledgeID: "second", KnowledgeFilename: "second.csv"}
	nonTable := &types.SearchResult{KnowledgeID: "text", KnowledgeFilename: "notes.pdf"}
	third := &types.SearchResult{KnowledgeID: "third", KnowledgeFilename: "third.xls"}
	fourth := &types.SearchResult{KnowledgeID: "fourth", KnowledgeFilename: "fourth.xlsx"}

	got := selectDataAnalysisTargets([]*types.SearchResult{firstChunk, duplicateFirst, duplicateUpload, second, nonTable, third, fourth}, nil, nil, 3)
	if len(got) != 3 || got[0] != firstChunk || got[1] != second || got[2] != third {
		t.Fatalf("expected the first three distinct ranked tables, got %#v", got)
	}
}

func TestSelectDataAnalysisTargetsPrefersExplicitTablesWithoutExceedingLimit(t *testing.T) {
	ranked := &types.SearchResult{KnowledgeID: "ranked", KnowledgeFilename: "ranked.xlsx"}
	explicitA := &types.SearchResult{KnowledgeID: "explicit-a", KnowledgeFilename: "a.xlsx"}
	explicitB := &types.SearchResult{KnowledgeID: "explicit-b", KnowledgeFilename: "b.xlsx"}
	targets := types.SearchTargets{{Type: types.SearchTargetTypeKnowledge, KnowledgeIDs: []string{"explicit-a", "explicit-b"}}}

	got := selectDataAnalysisTargets([]*types.SearchResult{ranked, explicitA, explicitB}, nil, targets, 3)
	if len(got) != 2 || got[0] != explicitA || got[1] != explicitB {
		t.Fatalf("expected only explicitly scoped tables in retrieval order, got %#v", got)
	}
}

func TestSelectDataAnalysisTargetsPrefersRerankedTableMetadata(t *testing.T) {
	row := &types.SearchResult{KnowledgeID: "row", KnowledgeFilename: "large.xlsx", ChunkType: string(types.ChunkTypeText)}
	column := &types.SearchResult{KnowledgeID: "column", KnowledgeFilename: "relevant.xlsx", ChunkType: string(types.ChunkTypeTableColumn)}

	got := selectDataAnalysisTargets([]*types.SearchResult{row, column}, nil, nil, 3)
	if len(got) != 2 || got[0] != column || got[1] != row {
		t.Fatalf("expected metadata first and remaining table candidates as fallback, got %#v", got)
	}
}

func TestExplicitDataAnalysisTargetDoesNotOverrideNonTableIntent(t *testing.T) {
	no := false
	table := &types.SearchResult{KnowledgeID: "selected", KnowledgeFilename: "records.xlsx"}
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{KnowledgeIDs: []string{"selected"}},
		PipelineState:   types.PipelineState{NeedsTableQuery: &no, SearchResult: []*types.SearchResult{table}},
	}
	if shouldAttemptDataAnalysis(manage) {
		t.Fatal("explicit file scope overrode the classified non-table intent")
	}
}

func TestDataAnalysisCandidatesUseRerankResults(t *testing.T) {
	search := &types.SearchResult{KnowledgeID: "search", KnowledgeFilename: "large.xlsx"}
	reranked := &types.SearchResult{KnowledgeID: "reranked", KnowledgeFilename: "relevant.xlsx"}
	scored := &types.SearchResult{KnowledgeID: "scored", KnowledgeFilename: "best.xlsx"}
	manage := &types.ChatManage{PipelineState: types.PipelineState{
		SearchResult:       []*types.SearchResult{search},
		RerankResult:       []*types.SearchResult{reranked},
		RerankScoredResult: []*types.SearchResult{scored},
	}}

	got := dataAnalysisCandidatesAfterRerank(manage)
	if len(got) != 1 || got[0] != scored {
		t.Fatalf("expected pre-MMR scored candidates, got %#v", got)
	}
}

func TestDataAnalysisCandidatesUseMMRResultsWhenScoredResultsUnavailable(t *testing.T) {
	reranked := &types.SearchResult{KnowledgeID: "reranked", KnowledgeFilename: "relevant.xlsx"}
	manage := &types.ChatManage{PipelineState: types.PipelineState{RerankResult: []*types.SearchResult{reranked}}}

	got := dataAnalysisCandidatesAfterRerank(manage)
	if len(got) != 1 || got[0] != reranked {
		t.Fatalf("expected MMR candidates, got %#v", got)
	}
}

func TestDataAnalysisCandidatesKeepAllRerankAcceptedTables(t *testing.T) {
	best := &types.SearchResult{KnowledgeID: "best", KnowledgeFilename: "best.xlsx", Score: 0.80}
	close := &types.SearchResult{KnowledgeID: "close", KnowledgeFilename: "close.xlsx", Score: 0.70}
	weak := &types.SearchResult{KnowledgeID: "weak", KnowledgeFilename: "weak.xlsx", Score: 0.60}
	text := &types.SearchResult{KnowledgeID: "text", KnowledgeFilename: "notes.pdf", Score: 0.40}
	manage := &types.ChatManage{PipelineState: types.PipelineState{RerankScoredResult: []*types.SearchResult{best, close, weak, text}}}

	got := dataAnalysisCandidatesAfterRerank(manage)
	if len(got) != 4 || got[0] != best || got[1] != close || got[2] != weak || got[3] != text {
		t.Fatalf("expected all rerank-accepted candidates to remain eligible, got %#v", got)
	}
}

func TestDataAnalysisCandidatesFallBackWhenRerankHasNoResults(t *testing.T) {
	search := &types.SearchResult{KnowledgeID: "search", KnowledgeFilename: "records.xlsx"}
	manage := &types.ChatManage{PipelineState: types.PipelineState{SearchResult: []*types.SearchResult{search}}}

	got := dataAnalysisCandidatesAfterRerank(manage)
	if len(got) != 1 || got[0] != search {
		t.Fatalf("expected search fallback, got %#v", got)
	}
}
