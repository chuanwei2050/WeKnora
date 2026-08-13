package chatpipeline

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestAssessGraphSkip_NoRoutingRelationNotNeeded(t *testing.T) {
	skip := AssessGraphSkip(&types.ChatManage{PipelineRequest: types.PipelineRequest{Query: "什么是向量数据库？"}})
	if skip.Layer1Allowed || skip.Reason != GraphReasonRelationNotNeeded {
		t.Fatalf("unexpected skip: %#v", skip)
	}
}

func TestAssessGraphSkip_RoutingBudgetDisabled(t *testing.T) {
	manage := &types.ChatManage{}
	manage.RoutingDecision = &types.RoutingDecision{
		Classification: types.QuestionComplexity{NeedsEntityRelation: true},
		Budget:         types.RoutingBudget{GraphEnabled: false},
	}
	skip := AssessGraphSkip(manage)
	if skip.Layer1Allowed || skip.Reason != GraphReasonRoutingBudgetDisabled {
		t.Fatalf("unexpected skip: %#v", skip)
	}
	if skip.ReasonLegacy != GraphReasonLegacyCombined {
		t.Fatalf("expected legacy combined reason, got %q", skip.ReasonLegacy)
	}
}

func TestAssessGraphSkip_NoGraphKB(t *testing.T) {
	manage := &types.ChatManage{PipelineRequest: types.PipelineRequest{Query: "A 和 B 是什么关系？"}}
	manage.Entity = []string{"A"}
	skip := AssessGraphSkip(manage)
	if !skip.Layer1Allowed || skip.Reason != GraphReasonNoGraphKB {
		t.Fatalf("unexpected skip: %#v", skip)
	}
}

func TestResolveGraphTelemetryReason_LegacyNoEvidence(t *testing.T) {
	skip := GraphSkipAssessment{Layer1Allowed: true, Reason: "", ReasonLegacy: ""}
	reason, legacy := ResolveGraphTelemetryReason(true, false, skip)
	if reason != GraphReasonNoGraphEvidence || legacy != GraphReasonLegacyNoEvidence {
		t.Fatalf("got reason=%q legacy=%q", reason, legacy)
	}
}
