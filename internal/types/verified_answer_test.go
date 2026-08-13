package types

import (
	"context"
	"testing"
)

func TestNormalizeModelIdentityIgnoresVersionPathAndCase(t *testing.T) {
	a := NormalizeModelIdentity("OpenAI", "HTTPS://MODEL.EXAMPLE/V1/", "judge", "1")
	b := NormalizeModelIdentity("openai", "https://model.example", "judge", "1")
	if a.Key() != b.Key() {
		t.Fatalf("identities should match: %q != %q", a.Key(), b.Key())
	}
}

func TestAggregateValidationCriticalIssueRequiresReflection(t *testing.T) {
	score, decision, err := AggregateValidation([]ValidationReport{{Model: NormalizeModelIdentity("test", "http://validator", "judge", "1"), FactScore: 1, LogicScore: 1, CitationScore: 1, CompletenessScore: 1, Issues: []ValidationIssue{{Severity: SeverityCritical}}}}, ValidationWeights{Fact: 1, Logic: 1, Citation: 1, Completeness: 1, PassThreshold: .8, ReflectThreshold: .6})
	if err != nil || decision != VerificationNeedsReview || score != 1 {
		t.Fatalf("unexpected aggregate: %.2f %s %v", score, decision, err)
	}
}

func TestVerificationBudgetIsAtomic(t *testing.T) {
	ledger := NewVerificationBudgetLedger(VerificationBudget{MaxModelCalls: 1})
	if err := ledger.Reserve(1, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Reserve(1, 0, 0, 0); err == nil {
		t.Fatal("expected second reservation to fail")
	}
}

func TestStructuredVerificationParsingBindsEvidence(t *testing.T) {
	bundle := EvidenceBundle{Items: []Evidence{{ID: "e1", Content: "fact"}}}
	draft, err := ParseDraftAnswer(`{"id":"d1","text":"answer","claims":[{"id":"c1","text":"fact","evidence_ids":["e1"],"core":true}]}`, bundle)
	if err != nil || draft.ID != "d1" {
		t.Fatalf("unexpected draft: %#v %v", draft, err)
	}
	if _, err := ParseDraftAnswer(`{"id":"d1","text":"answer","claims":[{"id":"c1","text":"fact","evidence_ids":["missing"],"core":true}]}`, bundle); err == nil {
		t.Fatal("expected unknown evidence reference to be rejected")
	}
}

func TestVerificationScopeRejectsWidenedEvidence(t *testing.T) {
	scope := VerificationScope{TenantID: 7, SessionID: "session", KnowledgeBaseIDs: []string{"kb"}, KnowledgeIDs: []string{"k1"}}
	bundle := EvidenceBundle{ScopeKey: scope.Key(), Items: []Evidence{{ID: "e", KnowledgeID: "k2"}}}
	if err := scope.ValidateBundle(bundle); err == nil {
		t.Fatal("expected evidence outside the parent knowledge scope to be rejected")
	}
}

func TestStructuredParsersRejectTrailingJSON(t *testing.T) {
	bundle := EvidenceBundle{Items: []Evidence{{ID: "e1"}}}
	if _, err := ParseDraftAnswer(`{"id":"d","text":"answer","claims":[]} {"extra":true}`, bundle); err == nil {
		t.Fatal("expected trailing JSON to be rejected")
	}
	if _, err := ParseReflectionPlan(`{"action":"stop","round":0} {"extra":true}`); err == nil {
		t.Fatal("expected trailing reflection JSON to be rejected")
	}
}

func TestConservativeResultPreservesVerificationAudit(t *testing.T) {
	config := VerifiedAnswerConfig{
		Enabled:        true,
		MaxReflections: 1,
		Weights: ValidationWeights{
			Fact: .35, Logic: .25, Citation: .25, Completeness: .15,
			PassThreshold: .8, ReflectThreshold: .6,
		},
		Budget: VerificationBudget{MaxModelCalls: 4, MaxParallelCalls: 1},
	}
	coordinator := NewVerifiedAnswerCoordinator(config)
	evidence := EvidenceBundle{Items: []Evidence{{ID: "e1", Content: "fact"}}}
	answer, err := coordinator.Execute(context.Background(), "query", nil, VerificationHooks{
		Retrieve: func(context.Context, string) (EvidenceBundle, error) {
			return evidence, nil
		},
		Draft: func(context.Context, string, EvidenceBundle) (DraftAnswer, error) {
			return DraftAnswer{ID: "draft", Text: "answer", Claims: []Claim{{ID: "claim", Text: "answer", EvidenceIDs: []string{"e1"}, Core: true}}}, nil
		},
		Validate: func(context.Context, DraftAnswer, EvidenceBundle) (ValidationReport, error) {
			return ValidationReport{Model: NormalizeModelIdentity("test", "http://validator", "judge", "1"), FactScore: .7, LogicScore: .7, CitationScore: .7, CompletenessScore: .7}, nil
		},
		Reflect: func(context.Context, DraftAnswer, []ValidationReport) (ReflectionPlan, error) {
			return ReflectionPlan{Action: ReflectionStop, Reason: "test"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if answer.Decision != VerificationConservative || answer.RetrievalCount != 1 || len(answer.Reports) != 1 {
		t.Fatalf("verification audit was lost: %+v", answer)
	}
	if len(answer.ReflectionActions) != 1 || answer.ReflectionActions[0] != string(ReflectionStop) {
		t.Fatalf("reflection action was lost: %+v", answer.ReflectionActions)
	}
}
