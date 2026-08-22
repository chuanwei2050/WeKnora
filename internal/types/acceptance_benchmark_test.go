package types

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type acceptanceJudgeStub struct{ raw string }

func (s acceptanceJudgeStub) Judge(_ context.Context, _ AcceptanceJudgeInput) (string, error) {
	return s.raw, nil
}

func TestResearchSuiteCoverageCannotBePartial(t *testing.T) {
	suite := AcceptanceSuiteVersion{Kind: AcceptanceSuiteResearch, RoutingTaxonomyID: "q", RoutingTaxonomyVersion: "1", Cases: []AcceptanceCase{{KnowledgeLayer: AcceptanceStandard, ComplexityLevel: ComplexityL1, AnswerKind: AcceptanceRefuse}}}
	if err := suite.Freeze(time.Now()); err == nil {
		t.Fatal("expected incomplete suite to be rejected")
	}
}

func TestResearchSuiteFreezeRequiresTurnAndAnswerCoverage(t *testing.T) {
	layers := []AcceptanceKnowledgeLayer{AcceptanceStandard, AcceptanceFoundation, AcceptanceInternal, AcceptanceExperience}
	levels := []ComplexityLevel{ComplexityL1, ComplexityL2, ComplexityL3, ComplexityL4}
	kinds := []AcceptanceAnswerKind{AcceptanceAnswerable, AcceptanceUnanswerable, AcceptanceRefuse}
	cases := make([]AcceptanceCase, 0, len(layers)*len(levels))
	index := 0
	for _, layer := range layers {
		for _, level := range levels {
			cases = append(cases, AcceptanceCase{ID: fmt.Sprintf("case-%d", index), KnowledgeLayer: layer, ComplexityLevel: level, ComplexitySubtype: SubtypeUnknown, MultiTurn: index%2 == 0, AnswerKind: kinds[index%len(kinds)]})
			index++
		}
	}
	suite := AcceptanceSuiteVersion{Kind: AcceptanceSuiteResearch, RoutingTaxonomyID: "q", RoutingTaxonomyVersion: "1", Cases: cases}
	if err := suite.Freeze(time.Now()); err != nil {
		t.Fatalf("complete research suite should freeze: %v", err)
	}
}

func TestAcceptanceAccuracyIsCaseBasedAndHumanGated(t *testing.T) {
	metrics := CalculateAcceptanceMetrics([]AcceptanceCaseResult{{CaseID: "multi", Passed: true, HumanReviewRequired: true}, {CaseID: "single", Passed: true}, {CaseID: "na", NotApplicable: true}}, nil, 15*time.Second)
	if metrics.Accuracy != .5 || metrics.Gate != GateIncomplete {
		t.Fatalf("unexpected metrics: %+v", metrics)
	}
}

func TestAcceptanceGateFailsWhenTTFTExceedsLimit(t *testing.T) {
	metrics := CalculateAcceptanceMetrics([]AcceptanceCaseResult{{CaseID: "ok", Passed: true}}, []int64{16000}, 15*time.Second)
	if metrics.Gate != GateFailed || metrics.TTFTOverLimit != 1 {
		t.Fatalf("expected TTFT gate failure: %+v", metrics)
	}
	metrics = CalculateAcceptanceMetrics(nil, []int64{100, 300}, time.Minute)
	if metrics.TTFTMedianMillis != 200 {
		t.Fatalf("expected even-sample median, got %d", metrics.TTFTMedianMillis)
	}
}

func TestAcceptanceJudgeCalibrationRequiresEnoughAgreement(t *testing.T) {
	machine := make([]bool, AcceptanceJudgeMinimumCalibrationSamples)
	expert := make([]bool, AcceptanceJudgeMinimumCalibrationSamples)
	for i := range expert {
		machine[i], expert[i] = i < 16, i < 16
	}
	calibration, err := CalculateAcceptanceJudgeCalibration(machine, expert)
	if err != nil || !calibration.Calibrated || calibration.AgreementRate != 1 {
		t.Fatalf("unexpected calibrated judge: %+v err=%v", calibration, err)
	}
	expert[0] = !expert[0]
	calibration, err = CalculateAcceptanceJudgeCalibration(machine, expert)
	if err != nil || calibration.Calibrated || calibration.Agreements != 19 {
		t.Fatalf("expected failed calibration: %+v err=%v", calibration, err)
	}
}

func TestAcceptanceReviewQueuePreservesMachineResult(t *testing.T) {
	results := []AcceptanceCaseResult{{
		CaseID: "case-1", Passed: true, HumanReviewRequired: true,
		Evaluation: &AcceptanceEvaluatorOutput{Evaluator: "judge", Version: "1", Score: .9, Passed: false},
	}}
	queue, err := BuildAcceptanceReviewQueue(results, .85, .95, map[string]bool{"case-1": true})
	if err != nil || len(queue) != 1 || len(queue[0].Reasons) != 3 || queue[0].MachineResult == nil || queue[0].MachineResult.Passed {
		t.Fatalf("unexpected review queue: %+v err=%v", queue, err)
	}
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := ApplyAcceptanceReview(&results[0], false, "reviewer-1", when); err != nil {
		t.Fatal(err)
	}
	if results[0].Passed || !results[0].HumanReviewed || results[0].Evaluation.Passed || results[0].HumanReviewedAt == nil {
		t.Fatalf("human decision did not preserve machine result: %+v", results[0])
	}
}

func TestModelAcceptanceEvaluatorKeepsJudgeIdentity(t *testing.T) {
	config := AcceptanceJudgeConfig{
		Model:            NormalizeModelIdentity("openai", "https://judge.example/v1", "judge", "2026-01"),
		PromptVersion:    "scale-v2",
		Evaluator:        "model-judge",
		EvaluatorVersion: "3",
		ScaleVersion:     "fixed-4d-v1",
	}
	evaluator := ModelAcceptanceEvaluator{Config: config, Judge: acceptanceJudgeStub{raw: `{"evaluator":"model-judge","version":"3","score":0.9,"passed":true,"dimensions":{"semantic_correctness":0.9,"faithfulness":0.9,"citation_accuracy":0.9,"citation_completeness":0.9},"human_review":false}`}}
	output, err := evaluator.EvaluateContext(context.Background(), AcceptanceCase{}, "answer", []string{"chunk-1"})
	if err != nil {
		t.Fatal(err)
	}
	if output.ModelIdentity.Key() != config.Model.Key() || output.PromptVersion != config.PromptVersion {
		t.Fatalf("judge metadata was not retained: %+v", output)
	}
	if key, err := config.VersionKey(); err != nil || key == "" {
		t.Fatalf("expected stable judge version key, key=%q err=%v", key, err)
	}
}

func TestModelAcceptanceEvaluatorRejectsIncompleteScale(t *testing.T) {
	config := AcceptanceJudgeConfig{
		Model:            NormalizeModelIdentity("local", "http://judge.internal/v1", "judge", "1"),
		PromptVersion:    "scale-v1",
		Evaluator:        "model-judge",
		EvaluatorVersion: "1",
		ScaleVersion:     "fixed-4d-v1",
	}
	evaluator := ModelAcceptanceEvaluator{Config: config, Judge: acceptanceJudgeStub{raw: `{"evaluator":"model-judge","version":"1","score":0.9,"dimensions":{"semantic_correctness":0.9},"human_review":false}`}}
	if _, err := evaluator.EvaluateContext(context.Background(), AcceptanceCase{}, "answer", nil); err == nil {
		t.Fatal("expected incomplete judge scale to be rejected")
	}
	if err := config.ValidateForGate(); err == nil {
		t.Fatal("expected uncalibrated judge to be rejected as a gate")
	}
}

func TestAcceptanceTTFTSamplesExcludeInternalFailures(t *testing.T) {
	accepted := time.Unix(100, 0)
	visible := accepted.Add(2 * time.Second)
	samples := AcceptanceTTFTSamples([]AcceptanceRequestTiming{
		{AcceptedAt: accepted, FirstVisibleAt: &visible},
		{AcceptedAt: accepted, FirstVisibleAt: &visible, TimedOut: true},
		{AcceptedAt: accepted, Error: "cancelled"},
	})
	if len(samples) != 1 || samples[0] != 2000 {
		t.Fatalf("unexpected TTFT samples: %v", samples)
	}
}

func TestAcceptanceTTFTBoundaryAcrossRAGAgentVerifiedAndErrorFlows(t *testing.T) {
	accepted := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		delta      time.Duration
		timedOut   bool
		errMessage string
		wantOK     bool
	}{
		{name: "rag exactly at boundary", delta: 15 * time.Second, wantOK: true},
		{name: "agent just over boundary", delta: 15*time.Second + time.Millisecond, wantOK: true},
		{name: "verified cancelled", delta: time.Second, errMessage: "cancelled", wantOK: false},
		{name: "error flow timed out", delta: time.Second, timedOut: true, wantOK: false},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			visible := accepted.Add(item.delta)
			timing := AcceptanceRequestTiming{AcceptedAt: accepted, FirstVisibleAt: &visible, TimedOut: item.timedOut, Error: item.errMessage}
			ttft, gotOK := timing.TTFTMillis()
			if gotOK != item.wantOK {
				t.Fatalf("TTFTMillis() ok=%v, want %v", gotOK, item.wantOK)
			}
			if item.name == "agent just over boundary" && CalculateAcceptanceMetrics([]AcceptanceCaseResult{{CaseID: item.name, Passed: true}}, []int64{ttft}, 15*time.Second).Gate != GateFailed {
				t.Fatal("TTFT over the 15 second boundary must fail the gate")
			}
		})
	}
}

func TestAcceptanceLoadScenarioRejectsUnsafeTargetsAndLimits(t *testing.T) {
	allowed := map[string]bool{"http://test-host": true}
	valid := LoadScenario{UserCount: 50, ConcurrentUsers: 10, Duration: time.Minute, Target: "http://test-host"}
	if err := valid.Validate(allowed); err != nil {
		t.Fatalf("valid load scenario rejected: %v", err)
	}
	unsafe := valid
	unsafe.ProductionConfirmed = true
	if err := unsafe.Validate(allowed); err == nil {
		t.Fatal("expected production-confirmed scenario to be rejected")
	}
	tooMany := valid
	tooMany.ConcurrentUsers = 11
	if err := tooMany.Validate(allowed); err == nil {
		t.Fatal("expected concurrency limit to be enforced")
	}
	unknownTarget := valid
	unknownTarget.Target = "http://unknown-target"
	if err := unknownTarget.Validate(allowed); err == nil {
		t.Fatal("expected target allowlist to be enforced")
	}
}

func TestSingleNodeRejectsPrivateNetworkComponent(t *testing.T) {
	err := ValidateAcceptanceProfile(AcceptanceRunSnapshot{Profile: AcceptanceProfileSingleNode, Components: []ComponentLocation{{Name: "chat", Required: true, Location: EndpointPrivateNetwork}}})
	if err == nil {
		t.Fatal("expected single-node gate failure")
	}
}

func TestTTFTRecorderIgnoresInternalAndEmptyChunks(t *testing.T) {
	var recorder TTFTRecorder
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	recorder.Accepted(start)
	recorder.FirstVisible("thinking", "private", start.Add(time.Second))
	recorder.FirstVisible("text", " ", start.Add(2*time.Second))
	recorder.FirstVisible("text", "visible", start.Add(3*time.Second))
	got, ok := recorder.TTFT()
	if !ok || got != 3*time.Second {
		t.Fatalf("unexpected TTFT: %v, %v", got, ok)
	}
}

func TestDeterministicEvaluatorHonorsRefusalAndEvidence(t *testing.T) {
	evaluator := DeterministicAcceptanceEvaluator{}
	refusal := evaluator.Evaluate(AcceptanceCase{AnswerKind: AcceptanceRefuse}, "I cannot answer this", nil)
	if !refusal.Passed {
		t.Fatalf("expected refusal case to pass: %+v", refusal)
	}
	missing := evaluator.Evaluate(AcceptanceCase{AnswerKind: AcceptanceAnswerable, EvidenceChunkIDs: []string{"c1"}}, "answer", nil)
	if missing.Passed || missing.Dimensions["citation_completeness"] != 0 {
		t.Fatalf("expected missing citation to fail: %+v", missing)
	}
}

func TestRecomputeAcceptanceCaseResultIgnoresSubmittedPassFlag(t *testing.T) {
	item := AcceptanceCase{ID: "case-1", AnswerKind: AcceptanceAnswerable, EvidenceChunkIDs: []string{"chunk-1"}}
	result := RecomputeAcceptanceCaseResult(item, AcceptanceCaseResult{CaseID: item.ID, Passed: true})
	if result.Passed || result.Error == "" {
		t.Fatalf("missing execution must not pass: %+v", result)
	}
	result = RecomputeAcceptanceCaseResult(item, AcceptanceCaseResult{
		CaseID: item.ID, Passed: false,
		Execution: &AcceptanceExecution{Answer: "answer", EvidenceChunkIDs: []string{"chunk-1"}, Timing: AcceptanceRequestTiming{AcceptedAt: time.Unix(1, 0), FirstVisibleAt: timePtr(time.Unix(2, 0)), CompletedAt: timePtr(time.Unix(3, 0))}},
	})
	if !result.Passed || result.Evaluation == nil {
		t.Fatalf("valid observed execution should pass: %+v", result)
	}
}

func timePtr(value time.Time) *time.Time { return &value }

func TestAcceptanceReportSummarizesRoutingGraphAndVerification(t *testing.T) {
	run := AcceptanceRun{ID: "run-1", Profile: AcceptanceProfileServerLoad}
	results := []AcceptanceCaseResult{
		{CaseID: "graph", Execution: &AcceptanceExecution{Routing: AcceptanceRoutingSnapshot{ExpectedLevel: ComplexityL3, ActualLevel: ComplexityL2, NeedsEntityRelation: true}, Graph: AcceptanceGraphSnapshot{Requested: true, Used: false}, VerificationPath: "verified_rag_postcheck"}},
		{CaseID: "fact", Execution: &AcceptanceExecution{Routing: AcceptanceRoutingSnapshot{ExpectedLevel: ComplexityL1, ActualLevel: ComplexityL1, NeedsEntityRelation: false}, Graph: AcceptanceGraphSnapshot{Used: true}, VerificationPath: "verified_agent"}},
	}
	report, err := BuildAcceptanceReport(run, results)
	if err != nil {
		t.Fatal(err)
	}
	if report.RoutingConfusion["L3"]["L2"] != 1 || report.GraphMissRate != .5 || report.GraphMisuseRate != .5 {
		t.Fatalf("unexpected telemetry summary: %+v", report)
	}
	if report.VerificationPaths["verified_agent"] != 1 || report.VerificationPaths["verified_rag_postcheck"] != 1 {
		t.Fatalf("unexpected verification path distribution: %+v", report.VerificationPaths)
	}
}

func TestAcceptanceEvaluatorOutputRejectsUnknownFields(t *testing.T) {
	if _, err := ParseAcceptanceEvaluatorOutput(`{"evaluator":"judge","version":"1","score":0.9,"dimensions":{"semantic_correctness":0.9},"unexpected":true}`); err == nil {
		t.Fatal("expected unknown evaluator field to be rejected")
	}
	output, err := ParseAcceptanceEvaluatorOutput(`{"evaluator":"judge","version":"1","score":0.9,"dimensions":{"semantic_correctness":0.9}}`)
	if err != nil || output.Score != 0.9 {
		t.Fatalf("unexpected evaluator output: %+v err=%v", output, err)
	}
}
