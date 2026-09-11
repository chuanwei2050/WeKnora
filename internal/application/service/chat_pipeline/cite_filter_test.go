package chatpipeline

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestNormalizeCiteFilterMode(t *testing.T) {
	cases := map[string]CiteFilterMode{
		"":              CiteFilterOff,
		"off":           CiteFilterOff,
		"OFF":           CiteFilterOff,
		"cited_or_all":  CiteFilterCitedOrAll,
		"CITED_ONLY":    CiteFilterCitedOnly,
		"unknown":       CiteFilterOff,
	}
	for raw, want := range cases {
		if got := NormalizeCiteFilterMode(raw); got != want {
			t.Fatalf("NormalizeCiteFilterMode(%q)=%q, want %q", raw, got, want)
		}
	}
}

func TestFilterSearchResultsByBracketCitations(t *testing.T) {
	cands := []*types.SearchResult{
		{ID: "a", Content: "one"},
		{ID: "b", Content: "two"},
		{ID: "c", Content: "three"},
	}

	got := FilterSearchResultsByBracketCitations(CiteFilterOff, "see [1]", cands)
	if len(got) != 3 {
		t.Fatalf("off mode should keep all, got %d", len(got))
	}

	got = FilterSearchResultsByBracketCitations(CiteFilterCitedOrAll, "答案见[1]与[3]。", cands)
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" {
		t.Fatalf("cited_or_all with cites: %#v", got)
	}

	got = FilterSearchResultsByBracketCitations(CiteFilterCitedOrAll, "无引用", cands)
	if len(got) != 3 {
		t.Fatalf("cited_or_all without cites should fallback, got %d", len(got))
	}

	got = FilterSearchResultsByBracketCitations(CiteFilterCitedOnly, "无引用", cands)
	if len(got) != 0 {
		t.Fatalf("cited_only without cites should be empty, got %d", len(got))
	}

	got = FilterSearchResultsByBracketCitations(CiteFilterCitedOrAll, "[0][99][2]", cands)
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("invalid indexes ignored: %#v", got)
	}
}

func TestFilterSearchResultsByChunkCitations(t *testing.T) {
	cands := []*types.SearchResult{
		{ID: "chunk-1"},
		{ID: "chunk-2"},
		{ID: "chunk-3"},
	}
	answer := `依据 <kb doc="d" chunk_id="chunk-1" /> 和 <kb chunk_id='chunk-3' />。`
	got := FilterSearchResultsByChunkCitations(CiteFilterCitedOrAll, answer, cands)
	if len(got) != 2 || got[0].ID != "chunk-1" || got[1].ID != "chunk-3" {
		t.Fatalf("chunk cite filter: %#v", got)
	}
}
