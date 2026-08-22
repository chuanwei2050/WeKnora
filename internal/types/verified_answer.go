package types

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
)

type ModelIdentity struct {
	ProtocolProvider string `json:"protocol_provider"`
	BaseEndpoint     string `json:"base_endpoint"`
	ModelName        string `json:"model_name"`
	Version          string `json:"version,omitempty"`
}

func NormalizeModelIdentity(protocolProvider, baseEndpoint, modelName, version string) ModelIdentity {
	baseEndpoint = strings.TrimRight(strings.TrimSpace(baseEndpoint), "/")
	if parsed, err := url.Parse(baseEndpoint); err == nil && parsed.Host != "" {
		parsed.Scheme = strings.ToLower(parsed.Scheme)
		parsed.Host = strings.ToLower(parsed.Host)
		parsed.Path = strings.TrimRight(parsed.Path, "/")
		for strings.HasSuffix(strings.ToLower(parsed.Path), "/v1") {
			parsed.Path = parsed.Path[:len(parsed.Path)-len("/v1")]
		}
		parsed.RawQuery, parsed.Fragment = "", ""
		baseEndpoint = strings.TrimRight(parsed.String(), "/")
	}
	return ModelIdentity{ProtocolProvider: strings.ToLower(strings.TrimSpace(protocolProvider)), BaseEndpoint: baseEndpoint, ModelName: strings.TrimSpace(modelName), Version: strings.TrimSpace(version)}
}

func (m ModelIdentity) Key() string {
	return strings.Join([]string{m.ProtocolProvider, m.BaseEndpoint, m.ModelName, m.Version}, "|")
}

type Evidence struct {
	ID                 string `json:"id"`
	Content            string `json:"content"`
	Source             string `json:"source,omitempty"`
	KnowledgeID        string `json:"knowledge_id,omitempty"`
	KnowledgeBaseID    string `json:"knowledge_base_id,omitempty"`
	KnowledgeVersionID string `json:"knowledge_version_id,omitempty"`
	ChunkID            string `json:"chunk_id,omitempty"`
}

type EvidenceBundle struct {
	ID       string     `json:"id"`
	Items    []Evidence `json:"items"`
	Query    string     `json:"query"`
	ScopeKey string     `json:"scope_key,omitempty"`
}

// VerificationScope is copied from the parent chat request. Every retrieval,
// draft and validator stage must keep the same tenant/session/knowledge scope;
// a validator is never allowed to widen it while reflecting.
type VerificationScope struct {
	TenantID           uint64   `json:"tenant_id"`
	SessionID          string   `json:"session_id"`
	KnowledgeBaseIDs   []string `json:"knowledge_base_ids,omitempty"`
	KnowledgeIDs       []string `json:"knowledge_ids,omitempty"`
	KnowledgeVersionID string   `json:"knowledge_version_id,omitempty"`
}

func (s VerificationScope) Key() string {
	bases := append([]string(nil), s.KnowledgeBaseIDs...)
	knowledges := append([]string(nil), s.KnowledgeIDs...)
	sort.Strings(bases)
	sort.Strings(knowledges)
	return fmt.Sprintf("%d|%s|%s|%s|%s", s.TenantID, strings.TrimSpace(s.SessionID), strings.Join(bases, ","), strings.Join(knowledges, ","), strings.TrimSpace(s.KnowledgeVersionID))
}

// RetrievalRequest is the only input a reflection round may use to request
// additional evidence. It deliberately carries the parent scope and routing
// snapshot so an adapter cannot silently broaden the search.
type RetrievalRequest struct {
	Query           string            `json:"query"`
	Reason          string            `json:"reason,omitempty"`
	Round           int               `json:"round"`
	Scope           VerificationScope `json:"scope"`
	RoutingDecision *RoutingDecision  `json:"routing_decision,omitempty"`
}

func (s VerificationScope) ValidateBundle(bundle EvidenceBundle) error {
	if s.TenantID == 0 || strings.TrimSpace(s.SessionID) == "" {
		return fmt.Errorf("verification scope is incomplete")
	}
	if bundle.ScopeKey != s.Key() {
		return fmt.Errorf("verification stage scope does not match parent request")
	}
	allowedKnowledge := map[string]bool{}
	allowedBase := map[string]bool{}
	for _, id := range s.KnowledgeIDs {
		allowedKnowledge[id] = true
	}
	for _, id := range s.KnowledgeBaseIDs {
		allowedBase[id] = true
	}
	for _, evidence := range bundle.Items {
		if len(allowedKnowledge) > 0 && evidence.KnowledgeID != "" && !allowedKnowledge[evidence.KnowledgeID] {
			return fmt.Errorf("verification evidence exceeds knowledge scope")
		}
		if s.KnowledgeVersionID != "" && evidence.KnowledgeVersionID != "" && evidence.KnowledgeVersionID != s.KnowledgeVersionID {
			return fmt.Errorf("verification evidence exceeds knowledge version scope")
		}
		if len(allowedBase) > 0 && evidence.KnowledgeBaseID != "" && !allowedBase[evidence.KnowledgeBaseID] {
			return fmt.Errorf("verification evidence exceeds knowledge base scope")
		}
	}
	return nil
}

type Claim struct {
	ID          string   `json:"id"`
	Text        string   `json:"text"`
	EvidenceIDs []string `json:"evidence_ids"`
	Core        bool     `json:"core"`
}

type DraftAnswer struct {
	ID     string  `json:"id"`
	Text   string  `json:"text"`
	Claims []Claim `json:"claims"`
}

func (d DraftAnswer) Validate(bundle EvidenceBundle) error {
	if strings.TrimSpace(d.Text) == "" {
		return fmt.Errorf("draft answer text is required")
	}
	known := make(map[string]bool, len(bundle.Items))
	for _, evidence := range bundle.Items {
		if evidence.ID != "" {
			known[evidence.ID] = true
		}
	}
	seenClaims := map[string]bool{}
	for _, claim := range d.Claims {
		if claim.ID == "" || seenClaims[claim.ID] {
			return fmt.Errorf("draft contains a duplicate or empty claim id")
		}
		seenClaims[claim.ID] = true
		if strings.TrimSpace(claim.Text) == "" {
			return fmt.Errorf("claim %q is empty", claim.ID)
		}
		if claim.Core && len(claim.EvidenceIDs) == 0 {
			return fmt.Errorf("core claim %q has no evidence", claim.ID)
		}
		for _, evidenceID := range claim.EvidenceIDs {
			if !known[evidenceID] {
				return fmt.Errorf("claim %q references unknown evidence %q", claim.ID, evidenceID)
			}
		}
	}
	return nil
}

func ParseDraftAnswer(raw string, bundle EvidenceBundle) (DraftAnswer, error) {
	var draft DraftAnswer
	if err := decodeStrictJSON(raw, &draft); err != nil {
		return DraftAnswer{}, fmt.Errorf("parse draft answer: %w", err)
	}
	if err := draft.Validate(bundle); err != nil {
		return DraftAnswer{}, err
	}
	return draft, nil
}

type ValidationDimension string

const (
	ValidationFact         ValidationDimension = "fact"
	ValidationLogic        ValidationDimension = "logic"
	ValidationCitation     ValidationDimension = "citation"
	ValidationCompleteness ValidationDimension = "completeness"
)

type ValidationSeverity string

const (
	SeverityInfo     ValidationSeverity = "info"
	SeverityWarning  ValidationSeverity = "warning"
	SeverityCritical ValidationSeverity = "critical"
)

type ValidationIssue struct {
	ClaimID     string              `json:"claim_id"`
	EvidenceIDs []string            `json:"evidence_ids"`
	Dimension   ValidationDimension `json:"dimension"`
	Severity    ValidationSeverity  `json:"severity"`
	Message     string              `json:"message,omitempty"`
}

type ValidationReport struct {
	ID                string            `json:"id"`
	Model             ModelIdentity     `json:"model"`
	FactScore         float64           `json:"fact_score"`
	LogicScore        float64           `json:"logic_score"`
	CitationScore     float64           `json:"citation_score"`
	CompletenessScore float64           `json:"completeness_score"`
	Issues            []ValidationIssue `json:"issues,omitempty"`
	Degraded          bool              `json:"degraded"`
	DegradationReason string            `json:"degradation_reason,omitempty"`
}

func (r ValidationReport) Validate() error {
	for _, score := range []float64{r.FactScore, r.LogicScore, r.CitationScore, r.CompletenessScore} {
		if score < 0 || score > 1 {
			return fmt.Errorf("validation scores must be between 0 and 1")
		}
	}
	if r.Model.Key() == "|||" {
		return fmt.Errorf("validation model identity is required")
	}
	for _, issue := range r.Issues {
		if issue.Dimension != ValidationFact && issue.Dimension != ValidationLogic && issue.Dimension != ValidationCitation && issue.Dimension != ValidationCompleteness {
			return fmt.Errorf("unknown validation dimension %q", issue.Dimension)
		}
	}
	return nil
}

func ParseValidationReport(raw string) (ValidationReport, error) {
	var report ValidationReport
	if err := decodeStrictJSON(raw, &report); err != nil {
		return ValidationReport{}, fmt.Errorf("parse validation report: %w", err)
	}
	if err := report.Validate(); err != nil {
		return ValidationReport{}, err
	}
	return report, nil
}

type ReflectionAction string

const (
	ReflectionRetrieveMore ReflectionAction = "retrieve_more"
	ReflectionRewrite      ReflectionAction = "rewrite"
	ReflectionStop         ReflectionAction = "stop"
)

type ReflectionPlan struct {
	Action ReflectionAction `json:"action"`
	Reason string           `json:"reason,omitempty"`
	Round  int              `json:"round"`
}

type VerificationDecision string

const (
	VerificationPassed       VerificationDecision = "passed"
	VerificationNeedsReview  VerificationDecision = "needs_reflection"
	VerificationConservative VerificationDecision = "conservative"
)

type VerifiedAnswer struct {
	Text              string               `json:"text"`
	Evidence          []Evidence           `json:"evidence"`
	Reports           []ValidationReport   `json:"reports,omitempty"`
	Decision          VerificationDecision `json:"decision"`
	Confidence        float64              `json:"confidence"`
	Degraded          bool                 `json:"degraded"`
	ConservativeNote  string               `json:"conservative_note,omitempty"`
	ExecutionPath     string               `json:"execution_path,omitempty"`
	ReflectionActions []string             `json:"reflection_actions,omitempty"`
	RetrievalCount    int                  `json:"retrieval_count,omitempty"`
}

// VerificationStageEvent is safe to expose to clients and traces. It contains
// lifecycle metadata and score summaries only; draft text, claims and private
// validator reasoning are intentionally absent.
type VerificationStageEvent struct {
	Stage          string               `json:"stage"`
	Status         string               `json:"status"`
	Decision       VerificationDecision `json:"decision,omitempty"`
	Confidence     float64              `json:"confidence,omitempty"`
	Degraded       bool                 `json:"degraded,omitempty"`
	DurationMillis int64                `json:"duration_ms,omitempty"`
	ModelKeys      []string             `json:"model_keys,omitempty"`
	Scores         map[string]float64   `json:"scores,omitempty"`
	Reason         string               `json:"reason,omitempty"`
}

type ValidationWeights struct {
	Fact             float64 `json:"fact"`
	Logic            float64 `json:"logic"`
	Citation         float64 `json:"citation"`
	Completeness     float64 `json:"completeness"`
	PassThreshold    float64 `json:"pass_threshold"`
	ReflectThreshold float64 `json:"reflect_threshold"`
}

func (w ValidationWeights) Validate() error {
	if w.Fact < 0 || w.Logic < 0 || w.Citation < 0 || w.Completeness < 0 {
		return fmt.Errorf("validation weights must not be negative")
	}
	if w.Fact+w.Logic+w.Citation+w.Completeness <= 0 {
		return fmt.Errorf("validation weights must not be empty")
	}
	if w.PassThreshold < 0 || w.PassThreshold > 1 || w.ReflectThreshold < 0 || w.ReflectThreshold > 1 || w.ReflectThreshold > w.PassThreshold {
		return fmt.Errorf("invalid validation thresholds")
	}
	return nil
}

func AggregateValidation(reports []ValidationReport, weights ValidationWeights) (float64, VerificationDecision, error) {
	if err := weights.Validate(); err != nil {
		return 0, VerificationConservative, err
	}
	if len(reports) == 0 {
		return 0, VerificationConservative, fmt.Errorf("at least one validation report is required")
	}
	var fact, logic, citation, complete float64
	for _, report := range reports {
		if err := report.Validate(); err != nil {
			if !(strings.Contains(err.Error(), "unknown validation dimension") && hasCriticalIssue(report)) {
				return 0, VerificationConservative, err
			}
		}
		fact += report.FactScore
		logic += report.LogicScore
		citation += report.CitationScore
		complete += report.CompletenessScore
	}
	count := float64(len(reports))
	fact, logic, citation, complete = fact/count, logic/count, citation/count, complete/count
	total := (fact*weights.Fact + logic*weights.Logic + citation*weights.Citation + complete*weights.Completeness) / (weights.Fact + weights.Logic + weights.Citation + weights.Completeness)
	for _, report := range reports {
		for _, issue := range report.Issues {
			if issue.Severity == SeverityCritical {
				return total, VerificationNeedsReview, nil
			}
		}
	}
	if total >= weights.PassThreshold {
		return total, VerificationPassed, nil
	}
	if total >= weights.ReflectThreshold {
		return total, VerificationNeedsReview, nil
	}
	return total, VerificationConservative, nil
}

func hasCriticalIssue(report ValidationReport) bool {
	for _, issue := range report.Issues {
		if issue.Severity == SeverityCritical {
			return true
		}
	}
	return false
}

type VerifiedAnswerConfig struct {
	Enabled                  bool               `json:"enabled"`
	StrictMultiModel         bool               `json:"strict_multi_model"`
	FactValidatorModelID     string             `json:"fact_validator_model_id,omitempty"`
	LogicValidatorModelID    string             `json:"logic_validator_model_id,omitempty"`
	CitationValidatorModelID string             `json:"citation_validator_model_id,omitempty"`
	Weights                  ValidationWeights  `json:"weights"`
	MaxReflections           int                `json:"max_reflections"`
	DegradationStrategy      string             `json:"degradation_strategy"`
	Budget                   VerificationBudget `json:"budget"`
}

// NormalizeLegacy maps the historical reflection flag to the new verification
// configuration. New agents default verification on in the editor / builtin YAML.
func (c *VerifiedAnswerConfig) NormalizeLegacy(reflectionEnabled bool) {
	if c == nil {
		return
	}
	if reflectionEnabled {
		c.Enabled = true
	}
	c.EnsureDefaults()
}

type VerificationBudget struct {
	MaxWallClockMillis int `json:"max_wall_clock_ms"`
	MaxModelCalls      int `json:"max_model_calls"`
	MaxInputTokens     int `json:"max_input_tokens"`
	MaxOutputTokens    int `json:"max_output_tokens"`
	MaxParallelCalls   int `json:"max_parallel_calls"`
}

func (c *VerifiedAnswerConfig) EnsureDefaults() {
	if c == nil {
		return
	}
	if c.MaxReflections == 0 {
		c.MaxReflections = 1
	}
	if c.MaxReflections > 2 {
		c.MaxReflections = 2
	}
	if c.DegradationStrategy == "" {
		c.DegradationStrategy = "conservative"
	}
	if c.Weights.PassThreshold == 0 {
		c.Weights = ValidationWeights{Fact: .35, Logic: .25, Citation: .25, Completeness: .15, PassThreshold: .8, ReflectThreshold: .6}
	}
	if c.Budget.MaxWallClockMillis == 0 {
		// Online multi-validator + reflection commonly exceeds 30s wall clock.
		c.Budget.MaxWallClockMillis = 120000
	}
	if c.Budget.MaxModelCalls == 0 {
		c.Budget.MaxModelCalls = 12
	}
	if c.Budget.MaxParallelCalls == 0 {
		c.Budget.MaxParallelCalls = 3
	}
}

func (c VerifiedAnswerConfig) Validate(identities []ModelIdentity) error {
	c.EnsureDefaults()
	if c.MaxReflections < 0 || c.MaxReflections > 2 {
		return fmt.Errorf("max_reflections must be between 0 and 2")
	}
	if err := c.Weights.Validate(); err != nil {
		return err
	}
	if !c.StrictMultiModel || len(identities) == 0 {
		return nil
	}
	seen := map[string]bool{}
	for _, identity := range identities {
		if identity.Key() == "|||" {
			continue
		}
		seen[identity.Key()] = true
	}
	if len(seen) < 2 {
		return fmt.Errorf("strict multi-model verification requires two distinct model identities")
	}
	return nil
}

type VerificationBudgetLedger struct {
	mu                                                   sync.Mutex
	config                                               VerificationBudget
	modelCalls, inputTokens, outputTokens, parallelCalls int
}

func NewVerificationBudgetLedger(config VerificationBudget) *VerificationBudgetLedger {
	return &VerificationBudgetLedger{config: config}
}

func (b *VerificationBudgetLedger) Reserve(modelCalls, inputTokens, outputTokens, parallelCalls int) error {
	if b == nil {
		return fmt.Errorf("verification budget is required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if modelCalls < 0 || inputTokens < 0 || outputTokens < 0 || parallelCalls < 0 {
		return fmt.Errorf("budget reservation must not be negative")
	}
	if b.config.MaxModelCalls > 0 && b.modelCalls+modelCalls > b.config.MaxModelCalls {
		return fmt.Errorf("model call budget exhausted")
	}
	if b.config.MaxInputTokens > 0 && b.inputTokens+inputTokens > b.config.MaxInputTokens {
		return fmt.Errorf("input token budget exhausted")
	}
	if b.config.MaxOutputTokens > 0 && b.outputTokens+outputTokens > b.config.MaxOutputTokens {
		return fmt.Errorf("output token budget exhausted")
	}
	if b.config.MaxParallelCalls > 0 && b.parallelCalls+parallelCalls > b.config.MaxParallelCalls {
		return fmt.Errorf("parallel call budget exhausted")
	}
	b.modelCalls += modelCalls
	b.inputTokens += inputTokens
	b.outputTokens += outputTokens
	b.parallelCalls += parallelCalls
	return nil
}

// ReleaseParallelCalls releases only the active parallelism reservation.
// Model calls and token usage remain consumed, while a later reflection round
// may reuse the same concurrency capacity after the current batch completes.
func (b *VerificationBudgetLedger) ReleaseParallelCalls(count int) {
	if b == nil || count <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if count > b.parallelCalls {
		b.parallelCalls = 0
		return
	}
	b.parallelCalls -= count
}

func (b *VerificationBudgetLedger) Snapshot() VerificationBudgetUsage {
	b.mu.Lock()
	defer b.mu.Unlock()
	return VerificationBudgetUsage{ModelCalls: b.modelCalls, InputTokens: b.inputTokens, OutputTokens: b.outputTokens, ParallelCalls: b.parallelCalls}
}

type VerificationBudgetUsage struct{ ModelCalls, InputTokens, OutputTokens, ParallelCalls int }

type VerificationBudgetEstimate struct {
	ModelCalls   int
	InputTokens  int
	OutputTokens int
}

// Keep context in the contract so coordinator implementations must propagate
// cancellation to retrieval and model adapters.
type VerificationHooks struct {
	Scope           *VerificationScope
	RoutingDecision *RoutingDecision
	// InitialUsage accounts for the answer-generation call that produced the
	// draft before verification started.
	InitialUsage             TokenUsage
	EstimateValidationBudget func(DraftAnswer, EvidenceBundle) VerificationBudgetEstimate
	EstimateReflectionBudget func(DraftAnswer, EvidenceBundle, []ValidationReport) VerificationBudgetEstimate
	EstimateRetrievalBudget  func(RetrievalRequest) VerificationBudgetEstimate
	Retrieve                 func(context.Context, string) (EvidenceBundle, error)
	RetrieveMore             func(context.Context, RetrievalRequest) (EvidenceBundle, error)
	Draft                    func(context.Context, string, EvidenceBundle) (DraftAnswer, error)
	Validate                 func(context.Context, DraftAnswer, EvidenceBundle) (ValidationReport, error)
	// ValidateMany is used when independent validator roles are dispatched in
	// parallel by the integration layer; every returned report is aggregated.
	ValidateMany func(context.Context, DraftAnswer, EvidenceBundle) ([]ValidationReport, error)
	Reflect      func(context.Context, DraftAnswer, []ValidationReport) (ReflectionPlan, error)
}
