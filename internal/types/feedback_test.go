package types

import "testing"

func TestFeedbackAdoptionRequiresRegisteredCapability(t *testing.T) {
	if err := ValidateFeedbackAdoption(FeedbackPending, FeedbackTargetKnowledgeDraft, FeedbackCapabilities{}); err == nil {
		t.Fatal("expected capability error")
	}
	if err := ValidateFeedbackAdoption(FeedbackPending, FeedbackTargetKnowledgeDraft, FeedbackCapabilities{KnowledgeGovernance: true}); err != nil {
		t.Fatal(err)
	}
}
