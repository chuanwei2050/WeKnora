package prompt

import (
	"strings"
	"testing"
)

func TestBuildFinalAnswerPromptConstraints(t *testing.T) {
	got := BuildFinalAnswerPrompt("谁持有证书？")
	wantSub := []string{
		"Based only on the retrieved evidence above",
		"User question: 谁持有证书？",
		"Never expose internal identifiers, raw retrieval payloads, tool names",
		"只输出面向用户的最终答案",
		"Only conclusions supported by evidence",
	}
	for _, s := range wantSub {
		if !strings.Contains(got, s) {
			t.Fatalf("missing %q in:\n%s", s, got)
		}
	}
}

func TestVerificationRewriteSystemConstraints(t *testing.T) {
	wantSub := []string{
		"Use only supplied evidence",
		"Do not expose physical table names",
		"raw result payloads",
		"structured-query process",
		"No analysis, validator commentary, chain-of-thought",
	}
	for _, s := range wantSub {
		if !strings.Contains(VerificationRewriteSystem, s) {
			t.Fatalf("missing %q in VerificationRewriteSystem:\n%s", s, VerificationRewriteSystem)
		}
	}
	if !strings.Contains(VerificationRewriteSystem, StructuredNoLeakRules) {
		t.Fatal("VerificationRewriteSystem must embed StructuredNoLeakRules")
	}
}

func TestFinalAnswerNoLeakRulesConstraints(t *testing.T) {
	wantSub := []string{
		"Never expose internal identifiers",
		"tool names",
		"physical table names",
		"structured-query process",
		"SQL",
	}
	for _, s := range wantSub {
		if !strings.Contains(FinalAnswerNoLeakRules, s) {
			t.Fatalf("missing %q in FinalAnswerNoLeakRules", s)
		}
	}
}

func TestIntentAwareImageAnalysisConstraints(t *testing.T) {
	got := IntentAwareImageAnalysis("这是什么")
	wantSub := []string{
		"用户的问题是：这是什么",
		"与用户问题相关",
		"用简洁的中文回答",
		"只输出分析结果",
	}
	for _, s := range wantSub {
		if !strings.Contains(got, s) {
			t.Fatalf("missing %q in:\n%s", s, got)
		}
	}
}

func TestWikiSummaryPromptConstraints(t *testing.T) {
	wantSub := []string{
		"SUMMARY:",
		"{{.Title}}",
		"{{.Content}}",
		"{{.Language}}",
		"[[slug|display name]]",
		"## Key Takeaways",
		"![caption](url)",
	}
	for _, s := range wantSub {
		if !strings.Contains(WikiSummaryPrompt, s) {
			t.Fatalf("missing %q in WikiSummaryPrompt", s)
		}
	}
}

func TestWikiChunkCitationPromptConstraints(t *testing.T) {
	wantSub := []string{
		`"citations"`,
		`"new_slugs"`,
		"{{.ChunksXML}}",
		"source_chunks",
		`{"citations": {}, "new_slugs": []}`,
	}
	for _, s := range wantSub {
		if !strings.Contains(WikiChunkCitationPrompt, s) {
			t.Fatalf("missing %q in WikiChunkCitationPrompt", s)
		}
	}
}

func TestWikiGranularityGuidance(t *testing.T) {
	if !strings.Contains(WikiGranularityGuidance("focused"), "FOCUSED") {
		t.Fatal("focused guidance missing FOCUSED")
	}
	if !strings.Contains(WikiGranularityGuidance("exhaustive"), "EXHAUSTIVE") {
		t.Fatal("exhaustive guidance missing EXHAUSTIVE")
	}
	if !strings.Contains(WikiGranularityGuidance(""), "STANDARD") {
		t.Fatal("default guidance missing STANDARD")
	}
}

func TestLLMRerankUserConstraints(t *testing.T) {
	got := LLMRerankUser("q", "p1", 2)
	wantSub := []string{
		"User Query: q",
		"Passage 1: X.XX",
		"Passage 2: X.XX",
		"Scores only",
		"0.0",
	}
	for _, s := range wantSub {
		if !strings.Contains(got, s) {
			t.Fatalf("missing %q in:\n%s", s, got)
		}
	}
}
