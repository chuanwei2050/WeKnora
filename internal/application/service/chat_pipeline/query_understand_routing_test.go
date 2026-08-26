package chatpipeline

import (
	"context"
	"strings"
	"testing"

	appconfig "github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
)

func TestParseStrictRoutingOutput(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "valid", raw: `{"complexity_level":"L3","reasoning_subtype":"multi_hop","needs_entity_relation":true,"confidence":0.91}`},
		{name: "markdown is rejected", raw: "```json\n{\"complexity_level\":\"L2\",\"reasoning_subtype\":\"contextual_fact\",\"confidence\":0.8}\n```", wantErr: true},
		{name: "missing confidence", raw: `{"complexity_level":"L2","reasoning_subtype":"contextual_fact"}`, wantErr: true},
		{name: "unknown level", raw: `{"complexity_level":"L5","reasoning_subtype":"contextual_fact","confidence":0.8}`, wantErr: true},
		{name: "unknown subtype", raw: `{"complexity_level":"L2","reasoning_subtype":"invented","confidence":0.8}`, wantErr: true},
		{name: "confidence out of range", raw: `{"complexity_level":"L2","reasoning_subtype":"contextual_fact","confidence":1.1}`, wantErr: true},
		{name: "wrong type", raw: `{"complexity_level":"L2","reasoning_subtype":"contextual_fact","confidence":"high"}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseStrictRoutingOutput(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %#v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.ComplexityLevel != types.ComplexityL3 || got.ReasoningSubtype != types.SubtypeMultiHop || !got.NeedsEntityRelation || got.Confidence != 0.91 {
				t.Fatalf("unexpected output: %#v", got)
			}
		})
	}
}

func TestPlanRoutingTable(t *testing.T) {
	base := types.DefaultComplexityRoutingConfig()
	tests := []struct {
		name       string
		input      types.QuestionComplexity
		capability func(*types.ComplexityRoutingConfig)
		planned    types.RoutingAction
		actual     types.RoutingAction
		reason     types.DegradationReason
	}{
		{name: "L1", input: types.QuestionComplexity{Level: types.ComplexityL1, Subtype: types.SubtypeExplicitFact, Confidence: .9}, planned: types.RoutingQuickRAG, actual: types.RoutingQuickRAG},
		{name: "L2", input: types.QuestionComplexity{Level: types.ComplexityL2, Subtype: types.SubtypeContextualFact, Confidence: .9}, planned: types.RoutingContextualRAG, actual: types.RoutingContextualRAG},
		{name: "L3 degrades", input: types.QuestionComplexity{Level: types.ComplexityL3, Subtype: types.SubtypeMultiHop, NeedsEntityRelation: true, Confidence: .9}, capability: func(c *types.ComplexityRoutingConfig) { c.Capabilities.GraphReasoning = false }, planned: types.RoutingGraphReasoning, actual: types.RoutingContextualRAG, reason: types.DegradationMissingCapability},
		{name: "L4 degrades without upgrade", input: types.QuestionComplexity{Level: types.ComplexityL4, Subtype: types.SubtypeCausal, NeedsEntityRelation: true, Confidence: .9}, capability: func(c *types.ComplexityRoutingConfig) {
			c.Capabilities.VerifiedAgent = false
			c.Capabilities.GraphReasoning = false
		}, planned: types.RoutingVerifiedAgent, actual: types.RoutingContextualRAG, reason: types.DegradationMissingCapability},
		{name: "low confidence", input: types.QuestionComplexity{Level: types.ComplexityL4, Subtype: types.SubtypeUnknown, Confidence: .1}, planned: types.RoutingContextualRAG, actual: types.RoutingContextualRAG, reason: types.DegradationLowConfidence},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			if tt.capability != nil {
				tt.capability(&cfg)
			}
			got := types.PlanRouting(tt.input, cfg)
			if got.PlannedAction != tt.planned || got.ActualAction != tt.actual || got.DegradationReason != tt.reason {
				t.Fatalf("got planned=%s actual=%s reason=%s", got.PlannedAction, got.ActualAction, got.DegradationReason)
			}
			if got.ActualAction == types.RoutingVerifiedAgent && !cfg.Capabilities.VerifiedAgent {
				t.Fatal("routing selected an unavailable capability")
			}
		})
	}
}

func TestPlanRoutingSkipsGraphWhenRelationNotNeeded(t *testing.T) {
	cfg := types.DefaultComplexityRoutingConfig()
	cfg.Capabilities.GraphReasoning = true
	decision := types.PlanRouting(types.QuestionComplexity{
		Level: types.ComplexityL3, Subtype: types.SubtypeComparison, Confidence: .9,
	}, cfg)
	if decision.ActualAction != types.RoutingContextualRAG || decision.Budget.GraphEnabled {
		t.Fatalf("graph route should be skipped when relation reasoning is unnecessary: %#v", decision)
	}
	if decision.DegradationReason != types.DegradationGraphNotNeeded {
		t.Fatalf("expected graph_not_needed, got %q", decision.DegradationReason)
	}
}

func TestRoutingConfigRejectsUpgradeChain(t *testing.T) {
	cfg := types.DefaultComplexityRoutingConfig()
	cfg.DegradationChains[types.RoutingContextualRAG] = []types.RoutingAction{types.RoutingContextualRAG, types.RoutingVerifiedAgent}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected upgrade chain to be rejected")
	}
}

func TestRoutingPromptUsesPreviousConversationForFollowUp(t *testing.T) {
	plugin := &PluginQueryUnderstand{config: &appconfig.Config{Conversation: &appconfig.ConversationConfig{
		RewritePromptSystem: "system",
		RewritePromptUser:   "history={{conversation}} query={{query}}",
	}}}
	manage := &types.ChatManage{PipelineRequest: types.PipelineRequest{Query: "它的发布日期呢？", ComplexityRouting: types.DefaultComplexityRoutingConfig()}}
	system, user := plugin.buildPrompts(manage, []*types.History{{Query: "介绍这份规范", Answer: "规范内容"}})
	if system == "" || !strings.Contains(user, "介绍这份规范") || !strings.Contains(user, "它的发布日期呢？") {
		t.Fatalf("follow-up context was not included: system=%q user=%q", system, user)
	}
}

func TestExplicitFactQueryUsesModelRouting(t *testing.T) {
	model := &validationChatStub{content: `{"rewrite_query":"系统集成项目管理工程师证书","intent":"kb_search","image_description":"","complexity_level":"L1","reasoning_subtype":"explicit_fact","needs_entity_relation":false,"confidence":0.95,"rationale_summary":"单条件事实检索"}`}
	plugin := &PluginQueryUnderstand{
		modelService: &validationModelServiceStub{chat: model},
		config: &appconfig.Config{Conversation: &appconfig.ConversationConfig{
			RewritePromptSystem: "system",
			RewritePromptUser:   "query={{query}}",
		}},
	}
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			Query:             "谁有系统集成项目管理工程师证书？",
			ChatModelID:       "chat-model",
			EnableRewrite:     true,
			ComplexityRouting: types.DefaultComplexityRoutingConfig(),
		},
		PipelineState: types.PipelineState{
			History: []*types.History{{Query: "", Answer: ""}},
		},
	}
	nextCalled := false
	if err := plugin.OnEvent(context.Background(), types.QUERY_UNDERSTAND, manage, func() *PluginError {
		nextCalled = true
		return nil
	}); err != nil {
		t.Fatalf("query understand failed: %v", err)
	}
	if model.calls != 1 || !nextCalled {
		t.Fatalf("explicit fact query bypassed model routing: calls=%d next=%v", model.calls, nextCalled)
	}
	if manage.RewriteQuery != "系统集成项目管理工程师证书" || manage.RoutingDecision == nil {
		t.Fatalf("model routing result was not applied: rewrite=%q routing=%#v", manage.RewriteQuery, manage.RoutingDecision)
	}
}

func TestShouldUseGraphRequiresRelationSignal(t *testing.T) {
	if ShouldUseGraph(&types.ChatManage{PipelineRequest: types.PipelineRequest{Query: "什么是向量数据库？"}}) {
		t.Fatal("ordinary fact question should not trigger graph")
	}
	if !ShouldUseGraph(&types.ChatManage{PipelineRequest: types.PipelineRequest{Query: "A 和 B 是什么关系？"}}) {
		t.Fatal("relation question should trigger graph fallback")
	}
	manage := &types.ChatManage{PipelineRequest: types.PipelineRequest{Query: "A 和 B 是什么关系？"}}
	manage.RoutingDecision = &types.RoutingDecision{
		Classification: types.QuestionComplexity{NeedsEntityRelation: false},
		Budget:         types.RoutingBudget{GraphEnabled: true},
	}
	if ShouldUseGraph(manage) {
		t.Fatal("explicit routing decision must override fallback relation signal")
	}
}

func TestClassifyQueryParseFailureAppliesConservativeBudget(t *testing.T) {
	model := &validationChatStub{content: "not-json"}
	appConfig := &appconfig.Config{Conversation: &appconfig.ConversationConfig{RewritePromptSystem: "system", RewritePromptUser: "query={{query}}"}}
	manage := &types.ChatManage{PipelineRequest: types.PipelineRequest{
		Query:                "比较两个方案",
		ComplexityRouting:    types.DefaultComplexityRoutingConfig(),
		EnableQueryExpansion: true,
		VerifiedAnswer:       types.VerifiedAnswerConfig{Enabled: true},
	}}
	manage.ComplexityRouting.Enabled = true
	_, err := ClassifyQuery(context.Background(), model, appConfig, manage)
	if err == nil {
		t.Fatal("expected strict routing parse error")
	}
	if manage.RoutingDecision == nil || manage.RoutingDecision.DegradationReason != types.DegradationParseFailed {
		t.Fatalf("expected conservative parse-failure decision: %#v", manage.RoutingDecision)
	}
	if !manage.EnableQueryExpansion || manage.VerifiedAnswer.Enabled {
		t.Fatalf("parse failure must apply contextual conservative budget: %+v", manage)
	}
}

func TestClassifyQueryParseFailurePreservesExplicitRelationRoute(t *testing.T) {
	model := &validationChatStub{content: `{"rewrite_query":"A和B的关系"}`}
	appConfig := &appconfig.Config{Conversation: &appconfig.ConversationConfig{RewritePromptSystem: "system", RewritePromptUser: "query={{query}}"}}
	manage := &types.ChatManage{PipelineRequest: types.PipelineRequest{
		Query:             "请查询A和B的关系",
		ComplexityRouting: types.DefaultComplexityRoutingConfig(),
	}}
	manage.ComplexityRouting.Enabled = true
	decision, err := ClassifyQuery(context.Background(), model, appConfig, manage)
	if err == nil {
		t.Fatal("expected strict routing parse error")
	}
	if decision == nil || decision.DegradationReason != types.DegradationParseFailed || decision.ActualAction != types.RoutingGraphReasoning {
		t.Fatalf("explicit relation signal must preserve graph fallback: %#v", decision)
	}
	if !decision.Classification.NeedsEntityRelation {
		t.Fatalf("expected relation classification: %#v", decision.Classification)
	}
}

func TestClassifyQueryMissingModelReturnsConservativeDecision(t *testing.T) {
	manage := &types.ChatManage{PipelineRequest: types.PipelineRequest{
		Query:                "比较两个方案",
		ComplexityRouting:    types.DefaultComplexityRoutingConfig(),
		EnableQueryExpansion: true,
	}}
	manage.ComplexityRouting.Enabled = true
	decision, err := ClassifyQuery(context.Background(), nil, &appconfig.Config{}, manage)
	if err == nil || decision == nil || decision.DegradationReason != types.DegradationMissingCapability {
		t.Fatalf("missing model must return conservative decision: decision=%#v err=%v", decision, err)
	}
	if !manage.EnableQueryExpansion || manage.VerifiedAnswer.Enabled {
		t.Fatalf("missing routing model must use contextual fallback without verification: %+v", manage)
	}
}
