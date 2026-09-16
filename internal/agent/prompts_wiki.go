package agent

import "github.com/Tencent/WeKnora/internal/prompt"

// Wiki prompt templates live in internal/prompt. Re-exported for existing call sites.

const (
	WikiSummaryPrompt = prompt.WikiSummaryPrompt
	WikiKnowledgeExtractPrompt = prompt.WikiKnowledgeExtractPrompt
	WikiCandidateSlugPrompt = prompt.WikiCandidateSlugPrompt
	WikiChunkCitationPrompt = prompt.WikiChunkCitationPrompt
	WikiPageModifyPrompt = prompt.WikiPageModifyPrompt
	WikiIndexIntroPrompt = prompt.WikiIndexIntroPrompt
	WikiIndexIntroUpdatePrompt = prompt.WikiIndexIntroUpdatePrompt
	WikiLogEntryTemplate = prompt.WikiLogEntryTemplate
	WikiDeduplicationPrompt = prompt.WikiDeduplicationPrompt
	WikiGranularityGuidanceFocused = prompt.WikiGranularityGuidanceFocused
	WikiGranularityGuidanceStandard = prompt.WikiGranularityGuidanceStandard
	WikiGranularityGuidanceExhaustive = prompt.WikiGranularityGuidanceExhaustive
)

// WikiGranularityGuidance returns the guidance text for the given granularity.
func WikiGranularityGuidance(granularity string) string {
	return prompt.WikiGranularityGuidance(granularity)
}
