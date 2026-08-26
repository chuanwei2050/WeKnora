package types

import "testing"

func TestPlanRoutingDegradesFromTargetWithoutUpgrade(t *testing.T) {
	cfg := DefaultComplexityRoutingConfig()
	if !cfg.Enabled {
		t.Fatal("default complexity routing must be enabled")
	}
	cfg.Capabilities.GraphReasoning = false
	decision := PlanRouting(QuestionComplexity{Level: ComplexityL3, Subtype: SubtypeMultiHop, NeedsEntityRelation: true, Confidence: .95}, cfg)
	if decision.PlannedAction != RoutingGraphReasoning || decision.ActualAction != RoutingContextualRAG {
		t.Fatalf("unexpected decision: %#v", decision)
	}
	if decision.DegradationReason != DegradationMissingCapability {
		t.Fatalf("expected missing capability, got %q", decision.DegradationReason)
	}
}

func TestPlanRoutingLowConfidenceUsesConservativeFallback(t *testing.T) {
	cfg := DefaultComplexityRoutingConfig()
	cfg.FallbackAction = RoutingVerifiedAgent
	decision := PlanRouting(QuestionComplexity{Level: ComplexityL4, Subtype: SubtypeUnknown, Confidence: .1}, cfg)
	if decision.ActualAction != RoutingContextualRAG {
		t.Fatalf("low confidence must not increase cost: %#v", decision)
	}
}

func TestApplyRoutingDecisionDoesNotBroadenScope(t *testing.T) {
	manage := &ChatManage{
		PipelineRequest: PipelineRequest{
			KnowledgeBaseIDs:     []string{"kb-authorized"},
			KnowledgeIDs:         []string{"knowledge-authorized"},
			EmbeddingTopK:        20,
			EnableQueryExpansion: false,
		},
	}
	manage.RoutingDecision = &RoutingDecision{
		ActualAction: RoutingVerifiedAgent,
		Budget:       RoutingBudget{RetrievalTopK: 50, QueryExpansion: true, GraphEnabled: true, VerificationEnabled: true},
	}
	manage.ApplyRoutingDecision()
	if len(manage.KnowledgeBaseIDs) != 1 || manage.KnowledgeBaseIDs[0] != "kb-authorized" {
		t.Fatalf("routing changed knowledge base scope: %#v", manage.KnowledgeBaseIDs)
	}
	if len(manage.KnowledgeIDs) != 1 || manage.KnowledgeIDs[0] != "knowledge-authorized" {
		t.Fatalf("routing changed knowledge scope: %#v", manage.KnowledgeIDs)
	}
	if manage.EmbeddingTopK != 20 {
		t.Fatalf("routing increased retrieval budget: %d", manage.EmbeddingTopK)
	}
	if manage.EnableQueryExpansion {
		t.Fatal("routing must not enable query expansion when the platform capability is disabled")
	}
	manage.RoutingDecision.Budget = RoutingBudget{GraphEnabled: true, MaxAgentIterations: 6, VerificationEnabled: true}
	manage.ApplyRoutingDecision()
	if !manage.VerifiedAnswer.Enabled || manage.VerifiedAnswer.Budget.MaxModelCalls != 6 {
		t.Fatalf("verification route budget was not applied: %+v", manage.VerifiedAnswer)
	}
}

func TestApplyRoutingDecisionConstrainsExistingOptionalStages(t *testing.T) {
	manage := &ChatManage{PipelineRequest: PipelineRequest{
		EnableQueryExpansion: true,
		VerifiedAnswer:       VerifiedAnswerConfig{Enabled: true},
	}}
	manage.RoutingDecision = &RoutingDecision{Budget: RoutingBudget{RetrievalTopK: 5}}
	manage.ApplyRoutingDecision()
	if manage.EnableQueryExpansion || manage.VerifiedAnswer.Enabled {
		t.Fatalf("routing budget must disable stages absent from the selected route: %+v", manage)
	}

	agent := &AgentConfig{VerifiedAnswer: VerifiedAnswerConfig{Enabled: true}}
	agent.RoutingDecision = &RoutingDecision{Budget: RoutingBudget{VerificationEnabled: false}}
	agent.ApplyRoutingDecision()
	if agent.VerifiedAnswer.Enabled {
		t.Fatal("agent route must not retain verification when the selected budget disables it")
	}
}
