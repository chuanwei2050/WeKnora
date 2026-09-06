package types

import "testing"

func TestUpgradeLegacyDefaultFallbackPrompt(t *testing.T) {
	current := currentDefaultFallbackPromptPrefix + "\n\n{{query}}"
	legacy := legacyDefaultFallbackPromptPrefix + "\n\n{{query}}"

	if got := UpgradeLegacyDefaultFallbackPrompt(legacy, current); got != current {
		t.Fatalf("legacy built-in prompt was not upgraded: %q", got)
	}
	custom := legacy + "\ncustom"
	if got := UpgradeLegacyDefaultFallbackPrompt(custom, current); got != custom {
		t.Fatalf("custom prompt was overwritten: %q", got)
	}
}

func TestChatManageClonePreservesKeywordQuery(t *testing.T) {
	manage := &ChatManage{PipelineState: PipelineState{
		RewriteQuery: "完整语义问题",
		KeywordQuery: "关键词主题",
	}}
	clone := manage.Clone()
	if clone.KeywordQuery != manage.KeywordQuery {
		t.Fatalf("keyword query was lost during clone: got %q", clone.KeywordQuery)
	}
}
