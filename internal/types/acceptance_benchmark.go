package types

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type AcceptanceSuiteKind string

const (
	AcceptanceSuiteResearch AcceptanceSuiteKind = "research_acceptance"
	AcceptanceSuiteRegular  AcceptanceSuiteKind = "regular"
)

type AcceptanceKnowledgeLayer string

const (
	AcceptanceStandard   AcceptanceKnowledgeLayer = "standard"
	AcceptanceFoundation AcceptanceKnowledgeLayer = "foundation"
	AcceptanceInternal   AcceptanceKnowledgeLayer = "internal"
	AcceptanceExperience AcceptanceKnowledgeLayer = "experience"
)

type AcceptanceAnswerKind string

const (
	AcceptanceAnswerable   AcceptanceAnswerKind = "answerable"
	AcceptanceUnanswerable AcceptanceAnswerKind = "unanswerable"
	AcceptanceRefuse       AcceptanceAnswerKind = "refuse"
)

type AcceptanceCase struct {
	ID                string                   `json:"id"`
	Question          string                   `json:"question,omitempty"`
	KnowledgeLayer    AcceptanceKnowledgeLayer `json:"knowledge_layer"`
	ComplexityLevel   ComplexityLevel          `json:"complexity_level"`
	ComplexitySubtype ReasoningSubtype         `json:"complexity_subtype"`
	MultiTurn         bool                     `json:"multi_turn"`
	AnswerKind        AcceptanceAnswerKind     `json:"answer_kind"`
	ReferenceAnswer   string                   `json:"reference_answer,omitempty"`
	RequiredClaims    []string                 `json:"required_claims,omitempty"`
	ForbiddenClaims   []string                 `json:"forbidden_claims,omitempty"`
	EvidenceChunkIDs  []string                 `json:"evidence_chunk_ids,omitempty"`
	Rounds            []AcceptanceRound        `json:"rounds,omitempty"`
}

type AcceptanceRound struct {
	Index    int    `json:"index"`
	Prompt   string `json:"prompt,omitempty"`
	Required bool   `json:"required"`
	Expected string `json:"expected,omitempty"`
	Passed   bool   `json:"passed"`
}

func (c AcceptanceCase) Validate() error {
	switch c.KnowledgeLayer {
	case AcceptanceStandard, AcceptanceFoundation, AcceptanceInternal, AcceptanceExperience:
	default:
		return fmt.Errorf("unknown knowledge layer %q", c.KnowledgeLayer)
	}
	if !validComplexityLevel(c.ComplexityLevel) || !validSubtype(c.ComplexitySubtype) {
		return fmt.Errorf("invalid complexity taxonomy for case %q", c.ID)
	}
	switch c.AnswerKind {
	case AcceptanceAnswerable, AcceptanceUnanswerable, AcceptanceRefuse:
	default:
		return fmt.Errorf("unknown answer kind %q", c.AnswerKind)
	}
	seenRounds := map[int]bool{}
	for _, round := range c.Rounds {
		if round.Index < 1 || seenRounds[round.Index] {
			return fmt.Errorf("invalid or duplicate round %d in case %q", round.Index, c.ID)
		}
		seenRounds[round.Index] = true
	}
	return nil
}

type AcceptanceSuiteVersion struct {
	ID                     string              `json:"id"`
	TenantID               uint64              `json:"tenant_id"`
	SuiteID                string              `json:"suite_id"`
	Version                string              `json:"version"`
	Kind                   AcceptanceSuiteKind `json:"kind"`
	RoutingTaxonomyID      string              `json:"routing_taxonomy_id"`
	RoutingTaxonomyVersion string              `json:"routing_taxonomy_version"`
	Cases                  []AcceptanceCase    `json:"cases"`
	Frozen                 bool                `json:"frozen"`
	FrozenAt               *time.Time          `json:"frozen_at,omitempty"`
	CreatedAt              time.Time           `json:"created_at"`
}

func (s *AcceptanceSuiteVersion) Freeze(now time.Time) error {
	if s == nil {
		return fmt.Errorf("suite version is required")
	}
	if len(s.Cases) == 0 {
		return fmt.Errorf("suite version must contain cases")
	}
	if s.Frozen {
		return fmt.Errorf("suite version is already frozen")
	}
	for _, item := range s.Cases {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	if s.RoutingTaxonomyID == "" || s.RoutingTaxonomyVersion == "" {
		return fmt.Errorf("routing taxonomy identity is required")
	}
	if s.Kind == AcceptanceSuiteResearch {
		layers := map[AcceptanceKnowledgeLayer]bool{}
		levels := map[ComplexityLevel]bool{}
		answerKinds := map[AcceptanceAnswerKind]bool{}
		hasSingleTurn, hasMultiTurn := false, false
		refuse := false
		for _, item := range s.Cases {
			if err := item.Validate(); err != nil {
				return err
			}
			layers[item.KnowledgeLayer] = true
			levels[item.ComplexityLevel] = true
			answerKinds[item.AnswerKind] = true
			hasMultiTurn = hasMultiTurn || item.MultiTurn
			hasSingleTurn = hasSingleTurn || !item.MultiTurn
			refuse = refuse || item.AnswerKind == AcceptanceRefuse
		}
		for _, layer := range []AcceptanceKnowledgeLayer{AcceptanceStandard, AcceptanceFoundation, AcceptanceInternal, AcceptanceExperience} {
			if !layers[layer] {
				return fmt.Errorf("research suite missing knowledge layer %q", layer)
			}
		}
		for _, level := range []ComplexityLevel{ComplexityL1, ComplexityL2, ComplexityL3, ComplexityL4} {
			if !levels[level] {
				return fmt.Errorf("research suite missing complexity level %q", level)
			}
		}
		if !refuse {
			return fmt.Errorf("research suite must contain a refusal case")
		}
		for _, answerKind := range []AcceptanceAnswerKind{AcceptanceAnswerable, AcceptanceUnanswerable, AcceptanceRefuse} {
			if !answerKinds[answerKind] {
				return fmt.Errorf("research suite missing answer kind %q", answerKind)
			}
		}
		if !hasSingleTurn || !hasMultiTurn {
			return fmt.Errorf("research suite must contain single-turn and multi-turn cases")
		}
	}
	s.Frozen, s.FrozenAt = true, &now
	return nil
}

type AcceptanceCaseResult struct {
	CaseID              string                     `json:"case_id"`
	RoundResults        []AcceptanceRoundResult    `json:"round_results,omitempty"`
	Execution           *AcceptanceExecution       `json:"execution,omitempty"`
	Passed              bool                       `json:"passed"`
	Error               string                     `json:"error,omitempty"`
	TimedOut            bool                       `json:"timed_out"`
	HumanReviewRequired bool                       `json:"human_review_required"`
	HumanReviewed       bool                       `json:"human_reviewed"`
	HumanReviewerID     string                     `json:"human_reviewer_id,omitempty"`
	HumanReviewedAt     *time.Time                 `json:"human_reviewed_at,omitempty"`
	NotApplicable       bool                       `json:"not_applicable"`
	Evaluation          *AcceptanceEvaluatorOutput `json:"evaluation,omitempty"`
}

// AcceptanceExecution is the immutable observation captured from the real
// RAG/Agent stream before an evaluator is applied. Keeping it beside the
// result prevents a caller from replacing telemetry with a hand-written pass
// flag and makes routing/graph/verification metrics reproducible.
type AcceptanceExecution struct {
	Answer            string                    `json:"answer,omitempty"`
	EvidenceChunkIDs  []string                  `json:"evidence_chunk_ids,omitempty"`
	CitationChunkIDs  []string                  `json:"citation_chunk_ids,omitempty"`
	Timing            AcceptanceRequestTiming   `json:"timing"`
	Routing           AcceptanceRoutingSnapshot `json:"routing"`
	Graph             AcceptanceGraphSnapshot   `json:"graph"`
	VerificationPath  string                    `json:"verification_path,omitempty"`
	DegradationReason string                    `json:"degradation_reason,omitempty"`
}

type AcceptanceRoutingSnapshot struct {
	ExpectedLevel       ComplexityLevel   `json:"expected_level,omitempty"`
	ExpectedSubtype     ReasoningSubtype  `json:"expected_subtype,omitempty"`
	ActualLevel         ComplexityLevel   `json:"actual_level,omitempty"`
	ActualSubtype       ReasoningSubtype  `json:"actual_subtype,omitempty"`
	NeedsEntityRelation bool              `json:"needs_entity_relation"`
	ActualAction        RoutingAction     `json:"actual_action,omitempty"`
	PlannedAction       RoutingAction     `json:"planned_action,omitempty"`
	DegradationReason   DegradationReason `json:"degradation_reason,omitempty"`
}

type AcceptanceGraphSnapshot struct {
	Requested bool   `json:"requested"`
	Used      bool   `json:"used"`
	Reason    string `json:"reason,omitempty"`
}

// RecomputeAcceptanceCaseResult derives the machine result from the observed
// stream. The caller-provided Passed flag is deliberately ignored.
func RecomputeAcceptanceCaseResult(item AcceptanceCase, result AcceptanceCaseResult) AcceptanceCaseResult {
	result.Passed = false
	if result.Execution == nil {
		if result.Error == "" {
			result.Error = "real execution observation is required"
		}
		return result
	}
	if result.Execution.Timing.Error != "" || result.Execution.Timing.TimedOut {
		result.Error = result.Execution.Timing.Error
		if result.Error == "" {
			result.Error = "execution timed out"
		}
		return result
	}
	if result.Execution.Answer == "" {
		result.Error = "real execution returned an empty answer"
		return result
	}
	if result.Execution.Timing.FirstVisibleAt == nil || result.Execution.Timing.CompletedAt == nil {
		result.Error = "real execution timing is incomplete"
		return result
	}
	evaluation := DeterministicAcceptanceEvaluator{}.Evaluate(item, result.Execution.Answer, result.Execution.EvidenceChunkIDs)
	result.Evaluation = &evaluation
	result.Passed = evaluation.Passed
	if !result.Passed && result.Error == "" {
		result.Error = evaluation.Reason
	}
	return result
}

type AcceptanceRun struct {
	ID             string                `json:"id"`
	TenantID       uint64                `json:"tenant_id"`
	SuiteVersionID string                `json:"suite_version_id"`
	Profile        AcceptanceProfile     `json:"profile"`
	Snapshot       AcceptanceRunSnapshot `json:"snapshot"`
	Metrics        AcceptanceMetrics     `json:"metrics"`
	Gate           AcceptanceGateStatus  `json:"gate"`
	CreatedAt      time.Time             `json:"created_at"`
}

type AcceptanceCaseResultRecord struct {
	ID      string               `json:"id"`
	RunID   string               `json:"run_id"`
	CaseID  string               `json:"case_id"`
	Payload AcceptanceCaseResult `json:"payload"`
}

type AcceptanceEvaluatorOutput struct {
	Evaluator           string                 `json:"evaluator"`
	Version             string                 `json:"version"`
	ModelIdentity       ModelIdentity          `json:"model_identity,omitempty"`
	Parameters          map[string]string      `json:"parameters,omitempty"`
	PromptVersion       string                 `json:"prompt_version,omitempty"`
	Score               float64                `json:"score"`
	Passed              bool                   `json:"passed"`
	Reason              string                 `json:"reason,omitempty"`
	EvidenceChunkIDs    []string               `json:"evidence_chunk_ids,omitempty"`
	Dimensions          map[string]float64     `json:"dimensions,omitempty"`
	HumanReview         bool                   `json:"human_review"`
	Raw                 map[string]interface{} `json:"raw,omitempty"`
	RawStructuredOutput map[string]interface{} `json:"raw_structured_output,omitempty"`
}

// AcceptanceJudgeConfig is immutable run metadata for a model-based judge.
// Its fingerprint must be included in a run snapshot whenever any judge input
// changes, preventing historical scores from being silently reinterpreted.
type AcceptanceJudgeConfig struct {
	Model            ModelIdentity     `json:"model"`
	Parameters       map[string]string `json:"parameters,omitempty"`
	PromptVersion    string            `json:"prompt_version"`
	Evaluator        string            `json:"evaluator"`
	EvaluatorVersion string            `json:"evaluator_version"`
	ScaleVersion     string            `json:"scale_version"`
	ReviewLow        float64           `json:"review_low"`
	ReviewHigh       float64           `json:"review_high"`
	CalibrationOK    bool              `json:"calibration_ok"`
}

func (c AcceptanceJudgeConfig) Validate() error {
	if c.Model.Key() == "|||" || strings.TrimSpace(c.Evaluator) == "" || strings.TrimSpace(c.EvaluatorVersion) == "" || strings.TrimSpace(c.PromptVersion) == "" || strings.TrimSpace(c.ScaleVersion) == "" {
		return fmt.Errorf("judge model, evaluator, prompt and scale identities are required")
	}
	if c.ReviewLow < 0 || c.ReviewLow > 1 || c.ReviewHigh < 0 || c.ReviewHigh > 1 || c.ReviewLow > c.ReviewHigh {
		return fmt.Errorf("invalid judge review boundaries")
	}
	return nil
}

// ValidateForGate is stricter than configuration validation: an uncalibrated
// judge may still be used to collect exploratory results, but it cannot be the
// sole source of an acceptance decision.
func (c AcceptanceJudgeConfig) ValidateForGate() error {
	if err := c.Validate(); err != nil {
		return err
	}
	if !c.CalibrationOK {
		return fmt.Errorf("uncalibrated acceptance judge cannot be used as a gate")
	}
	return nil
}

func (c AcceptanceJudgeConfig) VersionKey() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

type AcceptanceJudgeInput struct {
	Case        AcceptanceCase `json:"case"`
	Answer      string         `json:"answer"`
	EvidenceIDs []string       `json:"evidence_ids"`
}

type AcceptanceJudge interface {
	Judge(context.Context, AcceptanceJudgeInput) (string, error)
}

// ModelAcceptanceEvaluator turns a model's strict JSON envelope into the
// common evaluator output and keeps the raw structured result for audit.
type ModelAcceptanceEvaluator struct {
	Config AcceptanceJudgeConfig
	Judge  AcceptanceJudge
}

func (e ModelAcceptanceEvaluator) Name() string    { return e.Config.Evaluator }
func (e ModelAcceptanceEvaluator) Version() string { return e.Config.EvaluatorVersion }
func (e ModelAcceptanceEvaluator) Evaluate(item AcceptanceCase, answer string, evidenceChunkIDs []string) AcceptanceEvaluatorOutput {
	output, err := e.EvaluateContext(context.Background(), item, answer, evidenceChunkIDs)
	if err != nil {
		return AcceptanceEvaluatorOutput{Evaluator: e.Config.Evaluator, Version: e.Config.EvaluatorVersion, ModelIdentity: e.Config.Model, Parameters: e.Config.Parameters, PromptVersion: e.Config.PromptVersion, Reason: err.Error()}
	}
	return output
}

func (e ModelAcceptanceEvaluator) EvaluateContext(ctx context.Context, item AcceptanceCase, answer string, evidenceChunkIDs []string) (AcceptanceEvaluatorOutput, error) {
	if err := e.Config.Validate(); err != nil {
		return AcceptanceEvaluatorOutput{}, err
	}
	if e.Judge == nil {
		return AcceptanceEvaluatorOutput{}, fmt.Errorf("acceptance judge is required")
	}
	raw, err := e.Judge.Judge(ctx, AcceptanceJudgeInput{Case: item, Answer: answer, EvidenceIDs: append([]string(nil), evidenceChunkIDs...)})
	if err != nil {
		return AcceptanceEvaluatorOutput{}, err
	}
	output, err := ParseAcceptanceEvaluatorOutput(raw)
	if err != nil {
		return AcceptanceEvaluatorOutput{}, err
	}
	if output.Evaluator != e.Config.Evaluator || output.Version != e.Config.EvaluatorVersion {
		return AcceptanceEvaluatorOutput{}, fmt.Errorf("judge output identity does not match configured evaluator")
	}
	if err := ValidateModelJudgeDimensions(output.Dimensions); err != nil {
		return AcceptanceEvaluatorOutput{}, err
	}
	output.ModelIdentity = e.Config.Model
	output.Parameters = e.Config.Parameters
	output.PromptVersion = e.Config.PromptVersion
	var structured map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &structured); err == nil {
		output.RawStructuredOutput = structured
	}
	return output, nil
}

// ValidateModelJudgeDimensions keeps the fixed four-dimension scale stable
// across judge versions. Missing dimensions are not treated as zero because
// that would turn malformed model output into a silent gate failure.
func ValidateModelJudgeDimensions(dimensions map[string]float64) error {
	for _, dimension := range []string{"semantic_correctness", "faithfulness", "citation_accuracy", "citation_completeness"} {
		score, ok := dimensions[dimension]
		if !ok {
			return fmt.Errorf("model judge dimension %q is required", dimension)
		}
		if score < 0 || score > 1 {
			return fmt.Errorf("model judge dimension %q must be between 0 and 1", dimension)
		}
	}
	return nil
}

const (
	AcceptanceJudgeMinimumCalibrationSamples = 20
	// A judge that is the sole acceptance gate must agree with every expert
	// label in its frozen calibration subset.
	AcceptanceJudgeMinimumAgreementRate = 1.0
)

// AcceptanceJudgeCalibration records the reproducible comparison between a
// judge and an expert-labelled calibration subset. The result is metadata for
// the evaluator version; it is not inferred from the production run itself.
type AcceptanceJudgeCalibration struct {
	SampleCount   int     `json:"sample_count"`
	Agreements    int     `json:"agreements"`
	AgreementRate float64 `json:"agreement_rate"`
	Calibrated    bool    `json:"calibrated"`
}

func CalculateAcceptanceJudgeCalibration(machinePassed, expertPassed []bool) (AcceptanceJudgeCalibration, error) {
	if len(machinePassed) == 0 || len(machinePassed) != len(expertPassed) {
		return AcceptanceJudgeCalibration{}, fmt.Errorf("calibration labels must be non-empty and have equal length")
	}
	agreements := 0
	for i := range machinePassed {
		if machinePassed[i] == expertPassed[i] {
			agreements++
		}
	}
	rate := float64(agreements) / float64(len(machinePassed))
	return AcceptanceJudgeCalibration{
		SampleCount:   len(machinePassed),
		Agreements:    agreements,
		AgreementRate: rate,
		Calibrated:    len(machinePassed) >= AcceptanceJudgeMinimumCalibrationSamples && rate >= AcceptanceJudgeMinimumAgreementRate,
	}, nil
}

type AcceptanceReviewReason string

const (
	AcceptanceReviewBoundary AcceptanceReviewReason = "boundary_score"
	AcceptanceReviewConflict AcceptanceReviewReason = "judge_conflict"
	AcceptanceReviewSample   AcceptanceReviewReason = "sampled_case"
)

// AcceptanceReviewItem is a queue projection. The original machine result
// remains attached so a human decision can be audited without rewriting it.
type AcceptanceReviewItem struct {
	CaseID        string                     `json:"case_id"`
	Reasons       []AcceptanceReviewReason   `json:"reasons"`
	MachinePassed bool                       `json:"machine_passed"`
	MachineScore  float64                    `json:"machine_score"`
	MachineResult *AcceptanceEvaluatorOutput `json:"machine_result,omitempty"`
}

func BuildAcceptanceReviewQueue(results []AcceptanceCaseResult, reviewLow, reviewHigh float64, sampledCaseIDs map[string]bool) ([]AcceptanceReviewItem, error) {
	if reviewLow < 0 || reviewHigh > 1 || reviewLow > reviewHigh {
		return nil, fmt.Errorf("invalid review boundaries")
	}
	queue := make([]AcceptanceReviewItem, 0)
	for _, result := range results {
		if result.NotApplicable {
			continue
		}
		var reasons []AcceptanceReviewReason
		var score float64
		var machineResult *AcceptanceEvaluatorOutput
		if result.Evaluation != nil {
			score = result.Evaluation.Score
			copy := *result.Evaluation
			machineResult = &copy
			if result.Passed != result.Evaluation.Passed {
				reasons = append(reasons, AcceptanceReviewConflict)
			}
			if score >= reviewLow && score <= reviewHigh {
				reasons = append(reasons, AcceptanceReviewBoundary)
			}
		}
		if sampledCaseIDs[result.CaseID] {
			reasons = append(reasons, AcceptanceReviewSample)
		}
		if len(reasons) == 0 {
			continue
		}
		queue = append(queue, AcceptanceReviewItem{CaseID: result.CaseID, Reasons: reasons, MachinePassed: result.Passed, MachineScore: score, MachineResult: machineResult})
	}
	return queue, nil
}

func ApplyAcceptanceReview(result *AcceptanceCaseResult, passed bool, reviewerID string, reviewedAt time.Time) error {
	if result == nil {
		return fmt.Errorf("acceptance case result is required")
	}
	if !result.HumanReviewRequired {
		return fmt.Errorf("case does not require human review")
	}
	if strings.TrimSpace(reviewerID) == "" || reviewedAt.IsZero() {
		return fmt.Errorf("reviewer and review time are required")
	}
	result.Passed = passed
	result.HumanReviewed = true
	result.HumanReviewerID = reviewerID
	result.HumanReviewedAt = &reviewedAt
	return nil
}

func (o AcceptanceEvaluatorOutput) Validate() error {
	if strings.TrimSpace(o.Evaluator) == "" || strings.TrimSpace(o.Version) == "" {
		return fmt.Errorf("evaluator identity is required")
	}
	if o.Score < 0 || o.Score > 1 {
		return fmt.Errorf("evaluator score must be between 0 and 1")
	}
	for dimension, score := range o.Dimensions {
		switch dimension {
		case "semantic_correctness", "faithfulness", "citation_accuracy", "citation_completeness":
		default:
			return fmt.Errorf("unknown evaluator dimension %q", dimension)
		}
		if score < 0 || score > 1 {
			return fmt.Errorf("evaluator dimension %q must be between 0 and 1", dimension)
		}
	}
	return nil
}

// ParseAcceptanceEvaluatorOutput accepts only the versioned structured judge
// envelope; prose or unknown fields cannot silently become a gate result.
func ParseAcceptanceEvaluatorOutput(raw string) (AcceptanceEvaluatorOutput, error) {
	var output AcceptanceEvaluatorOutput
	if err := decodeStrictJSON(raw, &output); err != nil {
		return AcceptanceEvaluatorOutput{}, fmt.Errorf("parse evaluator output: %w", err)
	}
	if err := output.Validate(); err != nil {
		return AcceptanceEvaluatorOutput{}, err
	}
	return output, nil
}

// AcceptanceEvaluator is intentionally versioned so a changed judge or
// deterministic rule creates a new run snapshot instead of rewriting history.
type AcceptanceEvaluator interface {
	Name() string
	Version() string
	Evaluate(AcceptanceCase, string, []string) AcceptanceEvaluatorOutput
}

type DeterministicAcceptanceEvaluator struct{}

func (DeterministicAcceptanceEvaluator) Name() string    { return "deterministic-format-citation" }
func (DeterministicAcceptanceEvaluator) Version() string { return "1" }
func (DeterministicAcceptanceEvaluator) Evaluate(item AcceptanceCase, answer string, evidenceChunkIDs []string) AcceptanceEvaluatorOutput {
	output := AcceptanceEvaluatorOutput{Evaluator: DeterministicAcceptanceEvaluator{}.Name(), Version: "1", Dimensions: map[string]float64{}}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		output.Reason = "empty answer"
		return output
	}
	if item.AnswerKind == AcceptanceRefuse && !looksLikeRefusal(answer) {
		output.Reason = "case requires refusal"
		output.Dimensions["semantic_correctness"] = 0
		return output
	}
	if item.AnswerKind == AcceptanceUnanswerable && !looksLikeRefusal(answer) {
		output.Reason = "unanswerable case must be declined"
		output.Dimensions["semantic_correctness"] = 0
		return output
	}
	known := make(map[string]bool, len(evidenceChunkIDs))
	for _, id := range evidenceChunkIDs {
		known[id] = true
	}
	for _, required := range item.EvidenceChunkIDs {
		if !known[required] {
			output.Reason = "required evidence is missing"
			output.Dimensions["citation_completeness"] = 0
			return output
		}
	}
	output.Dimensions["semantic_correctness"] = 1
	output.Dimensions["faithfulness"] = 1
	output.Dimensions["citation_accuracy"] = 1
	output.Dimensions["citation_completeness"] = 1
	output.Score, output.Passed = 1, true
	return output
}

func looksLikeRefusal(answer string) bool {
	answer = strings.ToLower(answer)
	for _, marker := range []string{"无法回答", "无法确定", "不知道", "insufficient", "cannot answer", "can't answer", "refuse"} {
		if strings.Contains(answer, marker) {
			return true
		}
	}
	return false
}

type AcceptanceReport struct {
	Run               AcceptanceRun             `json:"run"`
	Results           []AcceptanceCaseResult    `json:"results"`
	RoutingConfusion  map[string]map[string]int `json:"routing_confusion,omitempty"`
	GraphMisuseRate   float64                   `json:"graph_misuse_rate"`
	GraphMissRate     float64                   `json:"graph_miss_rate"`
	VerificationPaths map[string]int            `json:"verification_path_distribution,omitempty"`
	Integrity         string                    `json:"integrity_sha256"`
}

func buildAcceptanceTelemetrySummary(results []AcceptanceCaseResult) (map[string]map[string]int, float64, float64, map[string]int) {
	confusion := map[string]map[string]int{}
	paths := map[string]int{}
	graphCases, graphMisuse, graphMiss := 0, 0, 0
	for _, result := range results {
		if result.Execution == nil {
			continue
		}
		execution := result.Execution
		expected := string(execution.Routing.ExpectedLevel)
		actual := string(execution.Routing.ActualLevel)
		if expected != "" && actual != "" {
			if confusion[expected] == nil {
				confusion[expected] = map[string]int{}
			}
			confusion[expected][actual]++
		}
		if execution.VerificationPath != "" {
			paths[execution.VerificationPath]++
		}
		graphCases++
		if !execution.Routing.NeedsEntityRelation && execution.Graph.Used {
			graphMisuse++
		}
		if execution.Routing.NeedsEntityRelation && !execution.Graph.Used && execution.Graph.Requested {
			graphMiss++
		}
	}
	if graphCases == 0 {
		return confusion, 0, 0, paths
	}
	return confusion, float64(graphMisuse) / float64(graphCases), float64(graphMiss) / float64(graphCases), paths
}

func FinalizeAcceptanceRun(run *AcceptanceRun, results []AcceptanceCaseResult, ttfts []int64, now time.Time) error {
	if run == nil {
		return fmt.Errorf("acceptance run is required")
	}
	if err := run.Snapshot.Validate(); err != nil {
		return err
	}
	run.Metrics = CalculateAcceptanceMetrics(results, ttfts, 15*time.Second)
	if len(results) == 0 {
		run.Metrics.Gate = GateIncomplete
	}
	run.Gate = run.Metrics.Gate
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	return nil
}

func BuildAcceptanceReport(run AcceptanceRun, results []AcceptanceCaseResult) (AcceptanceReport, error) {
	payload, err := json.Marshal(struct {
		Run     AcceptanceRun          `json:"run"`
		Results []AcceptanceCaseResult `json:"results"`
	}{run, results})
	if err != nil {
		return AcceptanceReport{}, err
	}
	sum := sha256.Sum256(payload)
	confusion, graphMisuse, graphMiss, paths := buildAcceptanceTelemetrySummary(results)
	return AcceptanceReport{
		Run: run, Results: results, RoutingConfusion: confusion,
		GraphMisuseRate: graphMisuse, GraphMissRate: graphMiss,
		VerificationPaths: paths, Integrity: hex.EncodeToString(sum[:]),
	}, nil
}

func (r AcceptanceReport) Markdown() string {
	return fmt.Sprintf("# Acceptance run %s\n\n- Profile: `%s`\n- Gate: `%s`\n- Accuracy: `%.4f`\n- TTFT P95: `%d ms`\n- Graph misuse rate: `%.4f`\n- Graph miss rate: `%.4f`\n- Verification paths: `%d`\n- Integrity SHA-256: `%s`\n", r.Run.ID, r.Run.Profile, r.Run.Gate, r.Run.Metrics.Accuracy, r.Run.Metrics.TTFTP95Millis, r.GraphMisuseRate, r.GraphMissRate, len(r.VerificationPaths), r.Integrity)
}

type AcceptanceRoundResult struct {
	RoundIndex int  `json:"round_index"`
	Passed     bool `json:"passed"`
}

type AcceptanceGateStatus string

const (
	GatePending    AcceptanceGateStatus = "pending"
	GateIncomplete AcceptanceGateStatus = "incomplete"
	GatePassed     AcceptanceGateStatus = "passed"
	GateFailed     AcceptanceGateStatus = "failed"
)

type AcceptanceMetrics struct {
	Accuracy           float64              `json:"accuracy"`
	TTFTMinMillis      int64                `json:"ttft_min_ms"`
	TTFTMedianMillis   int64                `json:"ttft_median_ms"`
	TTFTP95Millis      int64                `json:"ttft_p95_ms"`
	TTFTMaxMillis      int64                `json:"ttft_max_ms"`
	TTFTOverLimit      int                  `json:"ttft_over_limit"`
	Gate               AcceptanceGateStatus `json:"gate"`
	PendingHumanReview int                  `json:"pending_human_review"`
}

func CalculateAcceptanceMetrics(results []AcceptanceCaseResult, ttfts []int64, ttftLimit time.Duration) AcceptanceMetrics {
	metrics := AcceptanceMetrics{Gate: GatePending}
	denominator, passed, pending := 0, 0, 0
	for _, result := range results {
		if result.NotApplicable {
			continue
		}
		denominator++
		if result.Passed && (!result.HumanReviewRequired || result.HumanReviewed) {
			passed++
		}
		if result.HumanReviewRequired && !result.HumanReviewed {
			pending++
		}
	}
	if denominator > 0 {
		metrics.Accuracy = float64(passed) / float64(denominator)
	}
	metrics.PendingHumanReview = pending
	if len(ttfts) > 0 {
		sorted := append([]int64(nil), ttfts...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		metrics.TTFTMinMillis, metrics.TTFTMaxMillis = sorted[0], sorted[len(sorted)-1]
		middle := len(sorted) / 2
		if len(sorted)%2 == 0 {
			metrics.TTFTMedianMillis = (sorted[middle-1] + sorted[middle]) / 2
		} else {
			metrics.TTFTMedianMillis = sorted[middle]
		}
		index := int(float64(len(sorted)-1)*.95 + .5)
		metrics.TTFTP95Millis = sorted[index]
		for _, value := range sorted {
			if time.Duration(value)*time.Millisecond > ttftLimit {
				metrics.TTFTOverLimit++
			}
		}
	}
	if pending > 0 {
		metrics.Gate = GateIncomplete
	} else if metrics.TTFTOverLimit > 0 {
		metrics.Gate = GateFailed
	} else if metrics.Accuracy >= .9 {
		metrics.Gate = GatePassed
	} else {
		metrics.Gate = GateFailed
	}
	return metrics
}

type AcceptanceProfile string

const (
	AcceptanceProfileSingleNode AcceptanceProfile = "single-node"
	AcceptanceProfileServerLoad AcceptanceProfile = "server-load"
)

type ComponentLocation struct {
	Name     string           `json:"name"`
	Location EndpointLocation `json:"location"`
	Required bool             `json:"required"`
}
type AcceptanceRunSnapshot struct {
	SuiteVersionID         string              `json:"suite_version_id"`
	DatasetID              string              `json:"dataset_id"`
	DatasetVersion         string              `json:"dataset_version"`
	RoutingTaxonomyID      string              `json:"routing_taxonomy_id"`
	RoutingTaxonomyVersion string              `json:"routing_taxonomy_version"`
	ModelIdentities        []ModelIdentity     `json:"model_identities"`
	KnowledgeSnapshot      string              `json:"knowledge_snapshot"`
	AgentConfigSnapshot    string              `json:"agent_config_snapshot"`
	EvaluatorVersion       string              `json:"evaluator_version"`
	ThresholdSnapshot      string              `json:"threshold_snapshot"`
	EnvironmentSnapshot    string              `json:"environment_snapshot"`
	CodeVersion            string              `json:"code_version"`
	Profile                AcceptanceProfile   `json:"profile"`
	Components             []ComponentLocation `json:"components"`
}

// Validate ensures a run points at immutable inputs rather than silently
// measuring a moving target.
func (s AcceptanceRunSnapshot) Validate() error {
	if strings.TrimSpace(s.SuiteVersionID) == "" || strings.TrimSpace(s.DatasetID) == "" || strings.TrimSpace(s.DatasetVersion) == "" {
		return fmt.Errorf("suite and dataset identity are required")
	}
	if strings.TrimSpace(s.RoutingTaxonomyID) == "" || strings.TrimSpace(s.RoutingTaxonomyVersion) == "" {
		return fmt.Errorf("routing taxonomy identity is required")
	}
	if len(s.ModelIdentities) == 0 || strings.TrimSpace(s.EvaluatorVersion) == "" || strings.TrimSpace(s.ThresholdSnapshot) == "" || strings.TrimSpace(s.EnvironmentSnapshot) == "" || strings.TrimSpace(s.CodeVersion) == "" {
		return fmt.Errorf("model, evaluator, threshold, environment and code snapshots are required")
	}
	for _, identity := range s.ModelIdentities {
		if identity.Key() == "|||" {
			return fmt.Errorf("model identity snapshot is incomplete")
		}
	}
	return ValidateAcceptanceProfile(s)
}

// TTFTRecorder records only user-visible, non-empty answer data. Internal
// state, thinking text and empty stream chunks can never become TTFT samples.
type TTFTRecorder struct {
	mu        sync.Mutex
	accepted  time.Time
	firstText time.Time
	completed time.Time
}

type AcceptanceRequestTiming struct {
	AcceptedAt     time.Time  `json:"accepted_at"`
	FirstVisibleAt *time.Time `json:"first_visible_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	TTFTMS         int64      `json:"ttft_ms,omitempty"`
	TimedOut       bool       `json:"timed_out"`
	Error          string     `json:"error,omitempty"`
}

func (t AcceptanceRequestTiming) TTFTMillis() (int64, bool) {
	if t.TimedOut || t.Error != "" || t.FirstVisibleAt == nil || t.AcceptedAt.IsZero() || t.FirstVisibleAt.Before(t.AcceptedAt) {
		return 0, false
	}
	return t.FirstVisibleAt.Sub(t.AcceptedAt).Milliseconds(), true
}

func AcceptanceTTFTSamples(timings []AcceptanceRequestTiming) []int64 {
	samples := make([]int64, 0, len(timings))
	for _, timing := range timings {
		if value, ok := timing.TTFTMillis(); ok {
			samples = append(samples, value)
		}
	}
	return samples
}

func (r *TTFTRecorder) Accepted(at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.accepted.IsZero() {
		r.accepted = at
	}
}

func (r *TTFTRecorder) FirstVisible(kind, text string, at time.Time) {
	if strings.TrimSpace(text) == "" || kind == "internal" || kind == "thinking" || kind == "state" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.firstText.IsZero() {
		r.firstText = at
	}
}

func (r *TTFTRecorder) Completed(at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.completed.IsZero() {
		r.completed = at
	}
}

func (r *TTFTRecorder) TTFT() (time.Duration, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.accepted.IsZero() || r.firstText.IsZero() || r.firstText.Before(r.accepted) {
		return 0, false
	}
	return r.firstText.Sub(r.accepted), true
}

func (r *TTFTRecorder) CompletedAt() (time.Time, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.completed.IsZero() {
		return time.Time{}, false
	}
	return r.completed, true
}

func ValidateAcceptanceProfile(snapshot AcceptanceRunSnapshot) error {
	if snapshot.Profile != AcceptanceProfileSingleNode && snapshot.Profile != AcceptanceProfileServerLoad {
		return fmt.Errorf("unknown acceptance profile %q", snapshot.Profile)
	}
	if snapshot.Profile != AcceptanceProfileSingleNode {
		return nil
	}
	if len(snapshot.Components) == 0 {
		return fmt.Errorf("single-node requires component locations")
	}
	for _, component := range snapshot.Components {
		if component.Required && component.Location != EndpointSameHost {
			return fmt.Errorf("single-node requires %s to be same-host, got %s", component.Name, component.Location)
		}
	}
	return nil
}

type LoadScenario struct {
	UserCount           int           `json:"user_count"`
	ConcurrentUsers     int           `json:"concurrent_users"`
	Duration            time.Duration `json:"duration"`
	Target              string        `json:"target"`
	ProductionConfirmed bool          `json:"production_confirmed"`
}

func (s LoadScenario) Validate(allowedTargets map[string]bool) error {
	if s.UserCount < 1 || s.UserCount > 50 {
		return fmt.Errorf("user_count must be between 1 and 50")
	}
	if s.ConcurrentUsers < 1 || s.ConcurrentUsers > 10 || s.ConcurrentUsers > s.UserCount {
		return fmt.Errorf("concurrent_users must be between 1 and 10 and not exceed user_count")
	}
	if s.Duration <= 0 || s.Duration > time.Hour {
		return fmt.Errorf("duration must be between 1 second and 1 hour")
	}
	if !allowedTargets[s.Target] {
		return fmt.Errorf("target is not in the allowed test environment")
	}
	if s.ProductionConfirmed {
		return fmt.Errorf("production targets are not allowed")
	}
	return nil
}
