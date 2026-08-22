package chatpipeline

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestAppendComplexityFewShotExamples(t *testing.T) {
	base := "system"
	got := AppendComplexityFewShotExamples(base, nil, 0)
	if got != base {
		t.Fatalf("empty few-shot must keep prompt: %q", got)
	}
	got = AppendComplexityFewShotExamples(base, []types.ComplexityFewShot{
		{Question: "A与B关系？", Level: types.ComplexityL3, Subtype: types.SubtypeMultiHop},
		{Question: "什么是单元测试？", Level: types.ComplexityL1},
	}, 20)
	if !strings.Contains(got, "A与B关系？") || !strings.Contains(got, "complexity_level=L3") {
		t.Fatalf("expected few-shot injection, got %q", got)
	}
	got = AppendComplexityFewShotExamples(base, []types.ComplexityFewShot{
		{Question: "q1", Level: types.ComplexityL1},
		{Question: "q2", Level: types.ComplexityL2},
		{Question: "q3", Level: types.ComplexityL3},
	}, 2)
	if strings.Contains(got, "q3") {
		t.Fatalf("expected truncation at limit 2, got %q", got)
	}
}

func TestSummarizeAndRankGraphPathsForDisplay(t *testing.T) {
	result := &types.GraphSearchResult{
		Nodes: []types.CanonicalEntity{
			{CanonicalKey: "a", Name: "Alpha"},
			{CanonicalKey: "b", Name: "Beta"},
			{CanonicalKey: "c", Name: "Gamma"},
		},
		Paths: []types.GraphPath{
			{NodeKeys: []string{"a", "b"}, Edges: []types.GraphEdge{{RelationType: "uses"}}, Score: 0.4},
			{NodeKeys: []string{"a", "c"}, Edges: []types.GraphEdge{{RelationType: "tests"}}, Score: 0.9},
		},
	}
	summaries := SummarizeGraphPaths(result)
	if len(summaries) != 2 || summaries[0].Nodes[0] != "Alpha" {
		t.Fatalf("unexpected summaries: %#v", summaries)
	}
	unchanged := RankGraphPathsForDisplay(summaries, 0.9, false)
	if unchanged[0].ID != "a>b" {
		t.Fatalf("without verification order must stay: %#v", unchanged)
	}
	ranked := RankGraphPathsForDisplay(summaries, 1.0, true)
	if ranked[0].ID != "a>c" {
		t.Fatalf("expected higher score path first after rank: %#v", ranked)
	}
	// Ranking must not mutate the input slice order.
	if summaries[0].ID != "a>b" {
		t.Fatalf("input summaries mutated: %#v", summaries)
	}
}

func TestRankGraphPathsDoesNotTouchGraphContext(t *testing.T) {
	ctx := "entity A -[uses]-> entity B"
	result := &types.GraphSearchResult{
		Paths: []types.GraphPath{
			{NodeKeys: []string{"a", "b"}, Score: 0.2},
			{NodeKeys: []string{"c", "d"}, Score: 0.8},
		},
	}
	before := ctx
	_ = RankGraphPathsForDisplay(SummarizeGraphPaths(result), 1.0, true)
	if ctx != before {
		t.Fatalf("GraphContext string must stay unchanged")
	}
	if result.Paths[0].Score != 0.2 || result.Paths[1].Score != 0.8 {
		t.Fatalf("retrieval Paths must stay unchanged: %#v", result.Paths)
	}
}
