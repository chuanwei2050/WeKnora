package rageval

import (
	"math"
	"testing"
)

func TestEvaluateProducesQualityCostAndSliceMetrics(t *testing.T) {
	cases := []Case{
		{ID: "fact", Slices: map[string]string{"language": "zh", "type": "fact"}, RelevantEvidence: []string{"a"}, RankedEvidence: []string{"x", "a"}, ExpectedAnswer: "答案", ActualAnswer: "答案", Citations: []string{"a"}, LatencyMS: 100, PromptTokens: 20, CompletionTokens: 10, ExternalCalls: 2},
		{ID: "none", Slices: map[string]string{"language": "zh", "type": "no-answer"}, ExpectedNoAnswer: true, ActualNoAnswer: true, LatencyMS: 50, PromptTokens: 10, ExternalCalls: 1},
	}
	report := Evaluate("v1", map[string]any{"query_expansion": false}, cases, 2)
	if report.DatasetVersion != "v1" || report.Overall.Cases != 2 || len(report.Slices) != 3 {
		t.Fatalf("unexpected report shape: %+v", report)
	}
	if report.Overall.MRR != 0.5 || report.Overall.RecallAtK != 1 || report.Overall.NoAnswerAccuracy != 1 || report.Overall.AverageLatencyMS != 75 || report.Overall.AverageTokens != 20 || report.Overall.AverageExternalCalls != 1.5 {
		t.Fatalf("unexpected aggregate metrics: %+v", report.Overall)
	}
	if report.Overall.CitationPrecision != 1 {
		t.Fatalf("unexpected citation precision: %+v", report.Overall)
	}
}

func TestEvaluateDoesNotInflateNDCGForMissingRelevantResults(t *testing.T) {
	report := Evaluate("v1", nil, []Case{{
		RelevantEvidence: []string{"a", "b"}, RankedEvidence: []string{"a"},
	}}, 2)
	want := 1 / (1 + 1/math.Log2(3))
	if math.Abs(report.Overall.NDCG-want) > 1e-9 {
		t.Fatalf("nDCG = %v, want %v", report.Overall.NDCG, want)
	}
}

func TestEvaluateCountsMissingSourceRanksAsDisplacement(t *testing.T) {
	report := Evaluate("v1", nil, []Case{{
		RankedEvidence:          []string{"a"},
		ReferenceRankedEvidence: []string{"a", "b"},
		EvidenceSources:         map[string]string{"a": "wiki", "b": "faq"},
	}}, 2)
	if report.Overall.SourceDisplacementRate != 0.5 {
		t.Fatalf("source displacement = %v, want 0.5", report.Overall.SourceDisplacementRate)
	}
}
