package types

import "testing"

func TestPlanSubQuestionsIsBoundedAndOrdered(t *testing.T) {
	plan, err := PlanSubQuestions("比较两个方案的差异", QuestionComplexity{Level: ComplexityL4, Subtype: SubtypeComparison, Confidence: .9}, 4, 6, 30000)
	if err != nil || len(plan.Questions) != 1 {
		t.Fatalf("unexpected plan: %+v err=%v", plan, err)
	}
	plan = SubQuestionPlan{Questions: []SubQuestion{{Index: 1, Query: "先确认方案 A"}, {Index: 2, Query: "再比较", DependsOn: []int{1}}}, MaxQuestions: 4, MaxModelCalls: 6, MaxDurationMs: 30000}
	if err := plan.Validate(); err != nil {
		t.Fatalf("valid ordered plan rejected: %v", err)
	}
	plan.Questions = append(plan.Questions, SubQuestion{Index: 3, Query: "cycle", DependsOn: []int{3}})
	if err := plan.Validate(); err == nil {
		t.Fatal("expected self dependency to be rejected")
	}
}

func TestParseSubQuestionPlanRejectsUnknownFieldsAndKeepsDependencies(t *testing.T) {
	raw := `{"questions":[{"index":1,"query":"确认方案 A","required":true},{"index":2,"query":"比较方案 A 与 B","depends_on":[1],"required":true}]}`
	plan, err := ParseSubQuestionPlan(raw, "比较方案 A 与 B", 4, 6, 30000)
	if err != nil || len(plan.Questions) != 2 || len(plan.Questions[1].DependsOn) != 1 {
		t.Fatalf("unexpected parsed plan: %+v err=%v", plan, err)
	}
	if _, err := ParseSubQuestionPlan(`{"questions":[],"extra":true}`, "问题", 4, 6, 30000); err == nil {
		t.Fatal("expected strict plan parsing failure")
	}
}

func TestPlanSubQuestionsKeepsSimpleQuestionSingleStep(t *testing.T) {
	plan, err := PlanSubQuestions("版本号是多少？", QuestionComplexity{Level: ComplexityL1, Subtype: SubtypeExplicitFact, Confidence: .9}, 4, 4, 30000)
	if err != nil || len(plan.Questions) != 1 {
		t.Fatalf("simple question should remain one step: %+v err=%v", plan, err)
	}
}
