package chatpipeline

import (
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

// CiteFilterMode controls how knowledge references are filtered after answering.
type CiteFilterMode string

const (
	CiteFilterOff         CiteFilterMode = "off"
	CiteFilterCitedOrAll  CiteFilterMode = "cited_or_all"
	CiteFilterCitedOnly   CiteFilterMode = "cited_only"
)

var (
	bracketCitationRE = regexp.MustCompile(`\[(\d+)\]`)
	kbChunkIDRE       = regexp.MustCompile(`(?i)<kb\b[^>]*\bchunk_id\s*=\s*"([^"]+)"`)
	kbChunkIDSingleRE = regexp.MustCompile(`(?i)<kb\b[^>]*\bchunk_id\s*=\s*'([^']+)'`)
)

// ResolveCiteFilterMode reads CITE_FILTER_MODE (default off). Invalid values map to off.
func ResolveCiteFilterMode() CiteFilterMode {
	return NormalizeCiteFilterMode(os.Getenv("CITE_FILTER_MODE"))
}

// NormalizeCiteFilterMode normalizes a raw mode string.
func NormalizeCiteFilterMode(raw string) CiteFilterMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(CiteFilterCitedOrAll):
		return CiteFilterCitedOrAll
	case string(CiteFilterCitedOnly):
		return CiteFilterCitedOnly
	default:
		return CiteFilterOff
	}
}

// FilterSearchResultsByBracketCitations keeps candidates referenced by [n] (1-based).
// cited_or_all with no valid cites returns all candidates; cited_only returns empty.
func FilterSearchResultsByBracketCitations(mode CiteFilterMode, answer string, candidates []*types.SearchResult) []*types.SearchResult {
	if mode == CiteFilterOff || len(candidates) == 0 {
		return candidates
	}
	indexes := parseBracketCitationIndexes(answer)
	if len(indexes) == 0 {
		if mode == CiteFilterCitedOnly {
			return []*types.SearchResult{}
		}
		return candidates
	}
	picked := pickByIndexes(candidates, indexes)
	if len(picked) == 0 && mode == CiteFilterCitedOrAll {
		return candidates
	}
	return picked
}

// FilterSearchResultsByChunkCitations keeps candidates whose ID appears in <kb chunk_id="..."> markers.
func FilterSearchResultsByChunkCitations(mode CiteFilterMode, answer string, candidates []*types.SearchResult) []*types.SearchResult {
	if mode == CiteFilterOff || len(candidates) == 0 {
		return candidates
	}
	ids := parseCitedChunkIDs(answer)
	if len(ids) == 0 {
		if mode == CiteFilterCitedOnly {
			return []*types.SearchResult{}
		}
		return candidates
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		seen[id] = struct{}{}
	}
	out := make([]*types.SearchResult, 0, len(ids))
	added := make(map[string]struct{})
	for _, c := range candidates {
		if c == nil {
			continue
		}
		if _, ok := seen[c.ID]; !ok {
			continue
		}
		if _, dup := added[c.ID]; dup {
			continue
		}
		added[c.ID] = struct{}{}
		out = append(out, c)
	}
	if len(out) == 0 && mode == CiteFilterCitedOrAll {
		return candidates
	}
	return out
}

func parseBracketCitationIndexes(answer string) []int {
	matches := bracketCitationRE.FindAllStringSubmatch(answer, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[int]struct{})
	var indexes []int
	for _, m := range matches {
		n, err := strconv.Atoi(m[1])
		if err != nil || n <= 0 {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		indexes = append(indexes, n)
	}
	return indexes
}

func parseCitedChunkIDs(answer string) []string {
	var ids []string
	seen := make(map[string]struct{})
	add := func(re *regexp.Regexp) {
		for _, m := range re.FindAllStringSubmatch(answer, -1) {
			id := strings.TrimSpace(m[1])
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	add(kbChunkIDRE)
	add(kbChunkIDSingleRE)
	return ids
}

func pickByIndexes(candidates []*types.SearchResult, indexes []int) []*types.SearchResult {
	out := make([]*types.SearchResult, 0, len(indexes))
	seen := make(map[int]struct{})
	for _, idx := range indexes {
		if _, ok := seen[idx]; ok {
			continue
		}
		seen[idx] = struct{}{}
		if idx < 1 || idx > len(candidates) {
			continue
		}
		c := candidates[idx-1]
		if c == nil {
			continue
		}
		out = append(out, c)
	}
	return out
}

// CitationInstructionHint is appended to user content when cite filtering is enabled.
const CitationInstructionHint = "\n\nWhen you use evidence from a <context id=\"N\"> block, cite it inline as [N]. Only cite sources you actually used."
