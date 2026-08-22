package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestVerifiedAnswerCoordinatorKeepsDraftInternalUntilPass(t *testing.T) {
	config := types.VerifiedAnswerConfig{Enabled: true, MaxReflections: 1, Weights: types.ValidationWeights{Fact: 1, Logic: 1, Citation: 1, Completeness: 1, PassThreshold: .8, ReflectThreshold: .5}, Budget: types.VerificationBudget{MaxModelCalls: 6}}
	coordinator := NewVerifiedAnswerCoordinator(config)
	validationCalls := 0
	answer, err := coordinator.Execute(context.Background(), "question", nil, types.VerificationHooks{
		Retrieve: func(context.Context, string) (types.EvidenceBundle, error) {
			return types.EvidenceBundle{Items: []types.Evidence{{ID: "e1", Content: "evidence"}}}, nil
		},
		Draft: func(context.Context, string, types.EvidenceBundle) (types.DraftAnswer, error) {
			return types.DraftAnswer{Text: "final", Claims: []types.Claim{{ID: "c1", Text: "supported", EvidenceIDs: []string{"e1"}}}}, nil
		},
		Validate: func(context.Context, types.DraftAnswer, types.EvidenceBundle) (types.ValidationReport, error) {
			validationCalls++
			if validationCalls == 1 {
				return types.ValidationReport{Model: types.NormalizeModelIdentity("test", "http://validator", "v1", "1"), FactScore: .4, LogicScore: .4, CitationScore: .4, CompletenessScore: .4}, nil
			}
			return types.ValidationReport{Model: types.NormalizeModelIdentity("test", "http://validator", "v1", "1"), FactScore: 1, LogicScore: 1, CitationScore: 1, CompletenessScore: 1}, nil
		},
	})
	if err != nil || answer.Decision != types.VerificationPassed || answer.Text != "final" {
		t.Fatalf("unexpected answer: %+v, %v", answer, err)
	}
}

func TestVerifiedAnswerCoordinatorConservativeFailureModes(t *testing.T) {
	baseConfig := types.VerifiedAnswerConfig{
		Enabled: true, MaxReflections: 1,
		Weights: types.ValidationWeights{Fact: 1, Logic: 1, Citation: 1, Completeness: 1, PassThreshold: .8, ReflectThreshold: .5},
		Budget:  types.VerificationBudget{MaxModelCalls: 4},
	}
	validReport := func() types.ValidationReport {
		return types.ValidationReport{Model: types.NormalizeModelIdentity("test", "http://validator", "judge", "1"), FactScore: 1, LogicScore: 1, CitationScore: 1, CompletenessScore: 1}
	}
	baseHooks := func() types.VerificationHooks {
		return types.VerificationHooks{
			Retrieve: func(context.Context, string) (types.EvidenceBundle, error) {
				return types.EvidenceBundle{Items: []types.Evidence{{ID: "e1", KnowledgeID: "k1"}}}, nil
			},
			Draft: func(context.Context, string, types.EvidenceBundle) (types.DraftAnswer, error) {
				return types.DraftAnswer{Text: "draft", Claims: []types.Claim{{ID: "c1", Text: "claim", EvidenceIDs: []string{"e1"}}}}, nil
			},
			Validate: func(context.Context, types.DraftAnswer, types.EvidenceBundle) (types.ValidationReport, error) {
				return validReport(), nil
			},
		}
	}

	t.Run("scope mismatch", func(t *testing.T) {
		config := baseConfig
		coordinator := NewVerifiedAnswerCoordinator(config)
		hooks := baseHooks()
		scope := types.VerificationScope{TenantID: 1, SessionID: "s1", KnowledgeIDs: []string{"k1"}}
		hooks.Scope = &scope
		hooks.Retrieve = func(context.Context, string) (types.EvidenceBundle, error) {
			return types.EvidenceBundle{ScopeKey: "wrong", Items: []types.Evidence{{ID: "e1", KnowledgeID: "k1"}}}, nil
		}
		answer, err := coordinator.Execute(context.Background(), "q", nil, hooks)
		if err != nil || answer.Decision != types.VerificationConservative || !answer.Degraded {
			t.Fatalf("expected conservative scope failure: answer=%+v err=%v", answer, err)
		}
	})

	t.Run("evidence insufficient", func(t *testing.T) {
		coordinator := NewVerifiedAnswerCoordinator(baseConfig)
		hooks := baseHooks()
		hooks.Retrieve = func(context.Context, string) (types.EvidenceBundle, error) {
			return types.EvidenceBundle{}, nil
		}
		hooks.Validate = func(context.Context, types.DraftAnswer, types.EvidenceBundle) (types.ValidationReport, error) {
			return types.ValidationReport{Model: types.NormalizeModelIdentity("test", "http://validator", "judge", "1"), FactScore: 0, LogicScore: 0, CitationScore: 0, CompletenessScore: 0}, nil
		}
		answer, err := coordinator.Execute(context.Background(), "q", nil, hooks)
		if err != nil || answer.Decision != types.VerificationConservative || len(answer.Evidence) != 0 {
			t.Fatalf("expected conservative insufficient-evidence result: answer=%+v err=%v", answer, err)
		}
	})

	t.Run("citation conflict", func(t *testing.T) {
		coordinator := NewVerifiedAnswerCoordinator(baseConfig)
		hooks := baseHooks()
		hooks.Validate = func(context.Context, types.DraftAnswer, types.EvidenceBundle) (types.ValidationReport, error) {
			return types.ValidationReport{Model: types.NormalizeModelIdentity("test", "http://validator", "judge", "1"), FactScore: 1, LogicScore: 1, CitationScore: 0, CompletenessScore: 1}, nil
		}
		hooks.Reflect = func(context.Context, types.DraftAnswer, []types.ValidationReport) (types.ReflectionPlan, error) {
			return types.ReflectionPlan{Action: types.ReflectionStop, Reason: "citation conflict"}, nil
		}
		answer, err := coordinator.Execute(context.Background(), "q", nil, hooks)
		if err != nil || answer.Decision != types.VerificationConservative {
			t.Fatalf("expected conservative citation-conflict result: answer=%+v err=%v", answer, err)
		}
	})

	t.Run("validator failure", func(t *testing.T) {
		coordinator := NewVerifiedAnswerCoordinator(baseConfig)
		hooks := baseHooks()
		hooks.Validate = func(context.Context, types.DraftAnswer, types.EvidenceBundle) (types.ValidationReport, error) {
			return types.ValidationReport{}, context.Canceled
		}
		answer, err := coordinator.Execute(context.Background(), "q", nil, hooks)
		if err != nil || answer.Decision != types.VerificationConservative {
			t.Fatalf("expected conservative validator failure: answer=%+v err=%v", answer, err)
		}
	})

	t.Run("request cancellation", func(t *testing.T) {
		coordinator := NewVerifiedAnswerCoordinator(baseConfig)
		hooks := baseHooks()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		answer, err := coordinator.Execute(ctx, "q", nil, hooks)
		if err != nil || answer.Decision != types.VerificationConservative {
			t.Fatalf("expected conservative cancelled result: answer=%+v err=%v", answer, err)
		}
	})

	t.Run("model budget exhausted", func(t *testing.T) {
		config := baseConfig
		config.Budget.MaxModelCalls = 1
		coordinator := NewVerifiedAnswerCoordinator(config)
		answer, err := coordinator.Execute(context.Background(), "q", nil, baseHooks())
		if err != nil || answer.Decision != types.VerificationConservative {
			t.Fatalf("expected conservative budget failure: answer=%+v err=%v", answer, err)
		}
	})

	t.Run("retrieve more keeps scope and evidence", func(t *testing.T) {
		coordinator := NewVerifiedAnswerCoordinator(baseConfig)
		hooks := baseHooks()
		scope := types.VerificationScope{TenantID: 1, SessionID: "s1", KnowledgeIDs: []string{"k1"}}
		hooks.Scope = &scope
		hooks.Retrieve = func(context.Context, string) (types.EvidenceBundle, error) {
			return types.EvidenceBundle{ScopeKey: scope.Key(), Items: []types.Evidence{{ID: "e1", KnowledgeID: "k1"}}}, nil
		}
		calls := 0
		hooks.Validate = func(context.Context, types.DraftAnswer, types.EvidenceBundle) (types.ValidationReport, error) {
			calls++
			if calls == 1 {
				return types.ValidationReport{Model: types.NormalizeModelIdentity("test", "http://validator", "judge", "1"), FactScore: .4, LogicScore: .4, CitationScore: .4, CompletenessScore: .4}, nil
			}
			return validReport(), nil
		}
		hooks.Reflect = func(_ context.Context, _ types.DraftAnswer, _ []types.ValidationReport) (types.ReflectionPlan, error) {
			return types.ReflectionPlan{Action: types.ReflectionRetrieveMore}, nil
		}
		answer, err := coordinator.Execute(context.Background(), "q", nil, hooks)
		if err != nil || answer.Decision != types.VerificationPassed || len(answer.Evidence) != 1 {
			t.Fatalf("expected retrieved evidence to remain scoped: answer=%+v err=%v", answer, err)
		}
	})
}

func TestVerifiedAnswerCoordinatorReservesEstimatedTokenBudgetBeforeValidation(t *testing.T) {
	config := types.VerifiedAnswerConfig{
		Enabled: true,
		Budget:  types.VerificationBudget{MaxModelCalls: 4, MaxInputTokens: 10, MaxOutputTokens: 10},
	}
	coordinator := NewVerifiedAnswerCoordinator(config)
	called := false
	answer, err := coordinator.Execute(context.Background(), "q", nil, types.VerificationHooks{
		Retrieve: func(context.Context, string) (types.EvidenceBundle, error) { return types.EvidenceBundle{}, nil },
		Draft: func(context.Context, string, types.EvidenceBundle) (types.DraftAnswer, error) {
			return types.DraftAnswer{Text: "draft"}, nil
		},
		EstimateValidationBudget: func(types.DraftAnswer, types.EvidenceBundle) types.VerificationBudgetEstimate {
			return types.VerificationBudgetEstimate{InputTokens: 11, OutputTokens: 1}
		},
		Validate: func(context.Context, types.DraftAnswer, types.EvidenceBundle) (types.ValidationReport, error) {
			called = true
			return types.ValidationReport{}, nil
		},
	})
	if err != nil || answer.Decision != types.VerificationConservative || called {
		t.Fatalf("expected preflight token budget failure: answer=%+v err=%v called=%v", answer, err, called)
	}
}

func TestVerifiedAnswerCoordinatorUsesStructuredRetrievalAndRejectsNoNewEvidence(t *testing.T) {
	config := types.VerifiedAnswerConfig{
		Enabled: true, MaxReflections: 1,
		Weights: types.ValidationWeights{Fact: 1, Logic: 1, Citation: 1, Completeness: 1, PassThreshold: .9, ReflectThreshold: .5},
		Budget:  types.VerificationBudget{MaxModelCalls: 5},
	}
	scope := types.VerificationScope{TenantID: 7, SessionID: "session-1", KnowledgeBaseIDs: []string{"kb-1"}, KnowledgeVersionID: "version-1"}
	initial := types.EvidenceBundle{ScopeKey: scope.Key(), Items: []types.Evidence{{ID: "e1", KnowledgeID: "k1", KnowledgeBaseID: "kb-1", KnowledgeVersionID: "version-1"}}}
	structuredCalls := 0
	answer, err := types.NewVerifiedAnswerCoordinator(config).Execute(context.Background(), "q", nil, types.VerificationHooks{
		Scope:    &scope,
		Retrieve: func(context.Context, string) (types.EvidenceBundle, error) { return initial, nil },
		RetrieveMore: func(_ context.Context, request types.RetrievalRequest) (types.EvidenceBundle, error) {
			structuredCalls++
			if request.Scope.Key() != scope.Key() || request.Round != 1 || request.Reason != "need more" {
				t.Fatalf("unexpected structured request: %+v", request)
			}
			return types.EvidenceBundle{ScopeKey: scope.Key(), Items: []types.Evidence{{ID: "e1", KnowledgeID: "k1", KnowledgeBaseID: "kb-1", KnowledgeVersionID: "version-1"}}}, nil
		},
		EstimateRetrievalBudget: func(request types.RetrievalRequest) types.VerificationBudgetEstimate {
			if request.Scope.Key() != scope.Key() {
				t.Fatalf("retrieval estimate lost scope")
			}
			return types.VerificationBudgetEstimate{ModelCalls: 1}
		},
		Draft: func(context.Context, string, types.EvidenceBundle) (types.DraftAnswer, error) {
			return types.DraftAnswer{Text: "draft"}, nil
		},
		Validate: func(context.Context, types.DraftAnswer, types.EvidenceBundle) (types.ValidationReport, error) {
			return types.ValidationReport{Model: types.NormalizeModelIdentity("test", "http://validator", "judge", "1"), FactScore: .1, LogicScore: .1, CitationScore: .1, CompletenessScore: .1}, nil
		},
		Reflect: func(context.Context, types.DraftAnswer, []types.ValidationReport) (types.ReflectionPlan, error) {
			return types.ReflectionPlan{Action: types.ReflectionRetrieveMore, Round: 1, Reason: "need more"}, nil
		},
	})
	if err != nil || answer.Decision != types.VerificationConservative || structuredCalls != 1 {
		t.Fatalf("expected conservative no-new-evidence result: answer=%+v err=%v calls=%d", answer, err, structuredCalls)
	}
	if len(answer.ReflectionActions) != 2 || answer.ReflectionActions[1] != "no_new_evidence" {
		t.Fatalf("expected no_new_evidence audit marker: %+v", answer.ReflectionActions)
	}
}

func TestVerificationScopeRejectsWrongKnowledgeVersion(t *testing.T) {
	scope := types.VerificationScope{TenantID: 1, SessionID: "s", KnowledgeVersionID: "current"}
	if err := scope.ValidateBundle(types.EvidenceBundle{ScopeKey: scope.Key(), Items: []types.Evidence{{ID: "e", KnowledgeVersionID: "stale"}}}); err == nil {
		t.Fatal("expected stale knowledge version to be rejected")
	}
}
