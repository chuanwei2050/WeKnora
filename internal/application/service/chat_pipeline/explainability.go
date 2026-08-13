package chatpipeline

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

const defaultComplexityFewShotLimit = 20

// AppendComplexityFewShotExamples appends configured few-shot examples to a
// routing system prompt. Empty input is unchanged. Examples beyond limit are truncated.
func AppendComplexityFewShotExamples(systemPrompt string, examples []types.ComplexityFewShot, limit int) string {
	if len(examples) == 0 {
		return systemPrompt
	}
	if limit <= 0 {
		limit = defaultComplexityFewShotLimit
	}
	var b strings.Builder
	b.WriteString(systemPrompt)
	b.WriteString("\n\nFew-shot complexity examples (match level and subtype styles):\n")
	n := 0
	for _, example := range examples {
		question := strings.TrimSpace(example.Question)
		if question == "" || example.Level == "" {
			continue
		}
		n++
		if n > limit {
			break
		}
		line := fmt.Sprintf("- Q: %s => complexity_level=%s", question, example.Level)
		if example.Subtype != "" {
			line += fmt.Sprintf(", reasoning_subtype=%s", example.Subtype)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// GraphPathSummary is a UI/telemetry-safe path digest.
type GraphPathSummary struct {
	ID        string   `json:"id,omitempty"`
	Nodes     []string `json:"nodes"`
	Relations []string `json:"relations"`
	Score     float64  `json:"score"`
}

// SummarizeGraphPaths builds path summaries from structured GraphSearchResult.Paths.
func SummarizeGraphPaths(result *types.GraphSearchResult) []GraphPathSummary {
	if result == nil || len(result.Paths) == 0 {
		return nil
	}
	nameByKey := make(map[string]string, len(result.Nodes))
	for _, node := range result.Nodes {
		if strings.TrimSpace(node.CanonicalKey) == "" {
			continue
		}
		name := strings.TrimSpace(node.Name)
		if name == "" {
			name = node.CanonicalKey
		}
		nameByKey[node.CanonicalKey] = name
	}
	out := make([]GraphPathSummary, 0, len(result.Paths))
	for i, path := range result.Paths {
		nodes := make([]string, 0, len(path.NodeKeys))
		for _, key := range path.NodeKeys {
			if name, ok := nameByKey[key]; ok {
				nodes = append(nodes, name)
			} else if strings.TrimSpace(key) != "" {
				nodes = append(nodes, key)
			}
		}
		relations := make([]string, 0, len(path.Edges))
		for _, edge := range path.Edges {
			if typ := strings.TrimSpace(edge.RelationType); typ != "" {
				relations = append(relations, typ)
			}
		}
		id := fmt.Sprintf("path-%d", i+1)
		if len(path.NodeKeys) > 0 {
			id = strings.Join(path.NodeKeys, ">")
		}
		out = append(out, GraphPathSummary{
			ID:        id,
			Nodes:     nodes,
			Relations: relations,
			Score:     path.Score,
		})
	}
	return out
}

// RankGraphPathsForDisplay returns a stably reordered copy using verification confidence.
// Original path order is preserved when confidence is unavailable or there is at most one path.
func RankGraphPathsForDisplay(paths []GraphPathSummary, confidence float64, hasVerification bool) []GraphPathSummary {
	if !hasVerification || len(paths) <= 1 {
		return append([]GraphPathSummary(nil), paths...)
	}
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}
	type ranked struct {
		summary GraphPathSummary
		index   int
		weight  float64
	}
	items := make([]ranked, len(paths))
	for i, path := range paths {
		items[i] = ranked{
			summary: path,
			index:   i,
			weight:  path.Score * (0.5 + 0.5*confidence),
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].weight == items[j].weight {
			return items[i].index < items[j].index
		}
		return items[i].weight > items[j].weight
	})
	out := make([]GraphPathSummary, len(items))
	for i, item := range items {
		out[i] = item.summary
	}
	return out
}
