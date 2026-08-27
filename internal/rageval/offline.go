package rageval

import (
	"math"
	"sort"
)

type Case struct {
	ID                      string            `json:"id"`
	Slices                  map[string]string `json:"slices"`
	RelevantEvidence        []string          `json:"relevant_evidence"`
	RankedEvidence          []string          `json:"ranked_evidence"`
	ReferenceRankedEvidence []string          `json:"reference_ranked_evidence,omitempty"`
	EvidenceSources         map[string]string `json:"evidence_sources,omitempty"`
	ExpectedNoAnswer        bool              `json:"expected_no_answer"`
	ActualNoAnswer          bool              `json:"actual_no_answer"`
	ExpectedAnswer          string            `json:"expected_answer"`
	ActualAnswer            string            `json:"actual_answer"`
	Citations               []string          `json:"citations"`
	LatencyMS               int64             `json:"latency_ms"`
	PromptTokens            int               `json:"prompt_tokens"`
	CompletionTokens        int               `json:"completion_tokens"`
	ExternalCalls           int               `json:"external_calls"`
}

type Metrics struct {
	Cases                  int     `json:"cases"`
	RecallAtK              float64 `json:"recall_at_k"`
	MRR                    float64 `json:"mrr"`
	NDCG                   float64 `json:"ndcg"`
	NoAnswerAccuracy       float64 `json:"no_answer_accuracy"`
	CitationPrecision      float64 `json:"citation_precision"`
	ExactAnswerAccuracy    float64 `json:"exact_answer_accuracy"`
	SourceDisplacementRate float64 `json:"source_displacement_rate"`
	AverageLatencyMS       float64 `json:"average_latency_ms"`
	AverageTokens          float64 `json:"average_tokens"`
	AverageExternalCalls   float64 `json:"average_external_calls"`
}

type Report struct {
	DatasetVersion string             `json:"dataset_version"`
	ConfigSnapshot map[string]any     `json:"config_snapshot"`
	Overall        Metrics            `json:"overall"`
	Slices         map[string]Metrics `json:"slices"`
}

func Evaluate(datasetVersion string, config map[string]any, cases []Case, k int) Report {
	report := Report{DatasetVersion: datasetVersion, ConfigSnapshot: config, Slices: make(map[string]Metrics)}
	report.Overall = aggregate(cases, k)
	grouped := make(map[string][]Case)
	for _, item := range cases {
		keys := make([]string, 0, len(item.Slices))
		for key := range item.Slices {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			name := key + "=" + item.Slices[key]
			grouped[name] = append(grouped[name], item)
		}
	}
	for name, items := range grouped {
		report.Slices[name] = aggregate(items, k)
	}
	return report
}

func aggregate(cases []Case, k int) Metrics {
	var out Metrics
	retrievalCases := 0
	displacementCases := 0
	out.Cases = len(cases)
	if len(cases) == 0 {
		return out
	}
	for _, item := range cases {
		relevant := make(map[string]struct{}, len(item.RelevantEvidence))
		for _, id := range item.RelevantEvidence {
			relevant[id] = struct{}{}
		}
		limit := min(k, len(item.RankedEvidence))
		hits := 0
		dcg := 0.0
		firstRank := 0
		for i, id := range item.RankedEvidence[:limit] {
			if _, ok := relevant[id]; ok {
				hits++
				dcg += 1 / math.Log2(float64(i+2))
				if firstRank == 0 {
					firstRank = i + 1
				}
			}
		}
		if len(relevant) > 0 {
			retrievalCases++
			out.RecallAtK += float64(hits) / float64(len(relevant))
			idealHits := min(k, len(relevant))
			idcg := 0.0
			for i := 0; i < idealHits; i++ {
				idcg += 1 / math.Log2(float64(i+2))
			}
			if idcg > 0 {
				out.NDCG += dcg / idcg
			}
		}
		if firstRank > 0 {
			out.MRR += 1 / float64(firstRank)
		}
		if item.ExpectedNoAnswer == item.ActualNoAnswer {
			out.NoAnswerAccuracy++
		}
		citationHits := 0
		for _, id := range item.Citations {
			if _, ok := relevant[id]; ok {
				citationHits++
			}
		}
		if len(item.Citations) == 0 {
			if item.ExpectedNoAnswer {
				out.CitationPrecision++
			}
		} else {
			out.CitationPrecision += float64(citationHits) / float64(len(item.Citations))
		}
		if item.ExpectedAnswer == item.ActualAnswer {
			out.ExactAnswerAccuracy++
		}
		compareLimit := min(k, len(item.ReferenceRankedEvidence))
		if compareLimit > 0 {
			displacementCases++
			displaced := 0
			for i := 0; i < compareLimit; i++ {
				actualSource := "__missing__"
				if i < len(item.RankedEvidence) {
					actualSource = item.EvidenceSources[item.RankedEvidence[i]]
				}
				referenceSource := item.EvidenceSources[item.ReferenceRankedEvidence[i]]
				if actualSource != referenceSource {
					displaced++
				}
			}
			out.SourceDisplacementRate += float64(displaced) / float64(compareLimit)
		}
		out.AverageLatencyMS += float64(item.LatencyMS)
		out.AverageTokens += float64(item.PromptTokens + item.CompletionTokens)
		out.AverageExternalCalls += float64(item.ExternalCalls)
	}
	n := float64(len(cases))
	if retrievalCases > 0 {
		retrievalN := float64(retrievalCases)
		out.RecallAtK /= retrievalN
		out.MRR /= retrievalN
		out.NDCG /= retrievalN
	}
	out.NoAnswerAccuracy /= n
	out.CitationPrecision /= n
	out.ExactAnswerAccuracy /= n
	if displacementCases > 0 {
		out.SourceDisplacementRate /= float64(displacementCases)
	}
	out.AverageLatencyMS /= n
	out.AverageTokens /= n
	out.AverageExternalCalls /= n
	return out
}
