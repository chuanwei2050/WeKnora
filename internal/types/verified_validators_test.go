package types

import "testing"

func TestValidateDraftAnswerReportsUnsupportedCoreClaim(t *testing.T) {
	report := ValidateDraftAnswer(DraftAnswer{
		ID:     "draft",
		Text:   "answer",
		Claims: []Claim{{ID: "claim-1", Text: "unsupported", Core: true}},
	}, EvidenceBundle{Items: []Evidence{{ID: "e-1"}}}, NormalizeModelIdentity("test", "http://validator", "v", "1"))
	if report.CitationScore != 0 || len(report.Issues) != 1 || report.Issues[0].Severity != SeverityCritical {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestParseReflectionPlanRejectsUnknownFieldsAndActions(t *testing.T) {
	if _, err := ParseReflectionPlan(`{"action":"rewrite","round":1,"private":"chain"}`); err == nil {
		t.Fatal("expected unknown field to be rejected")
	}
	if _, err := ParseReflectionPlan(`{"action":"guess","round":1}`); err == nil {
		t.Fatal("expected unknown action to be rejected")
	}
	plan, err := ParseReflectionPlan(`{"action":"stop","round":1}`)
	if err != nil || plan.Action != ReflectionStop {
		t.Fatalf("unexpected plan: %+v, %v", plan, err)
	}
}
