package types

import (
	"encoding/json"

	"fmt"
	"gopkg.in/yaml.v3"
	"math"
	"sort"
	"strings"
)

// ComplexityLevel is the stable four-level question taxonomy.
type ComplexityLevel string

const (
	ComplexityL1 ComplexityLevel = "L1"
	ComplexityL2 ComplexityLevel = "L2"
	ComplexityL3 ComplexityLevel = "L3"
	ComplexityL4 ComplexityLevel = "L4"
)

// ReasoningSubtype is a compact, auditable routing hint. It never contains
// model chain-of-thought.
type ReasoningSubtype string

const (
	SubtypeExplicitFact   ReasoningSubtype = "explicit_fact"
	SubtypeContextualFact ReasoningSubtype = "contextual_fact"
	SubtypeComparison     ReasoningSubtype = "comparison"
	SubtypeMultiHop       ReasoningSubtype = "multi_hop"
	SubtypeCausal         ReasoningSubtype = "causal"
	SubtypeHypothetical   ReasoningSubtype = "hypothetical"
	SubtypeTransfer       ReasoningSubtype = "transfer"
	SubtypeUnknown        ReasoningSubtype = "unknown"
)

// RoutingAction is finite so routing can only select existing capabilities.
type RoutingAction string

const (
	RoutingQuickRAG       RoutingAction = "quick_rag"
	RoutingContextualRAG  RoutingAction = "contextual_rag"
	RoutingGraphReasoning RoutingAction = "graph_reasoning"
	RoutingVerifiedAgent  RoutingAction = "verified_agent"
)

type DegradationReason string

const (
	DegradationNone              DegradationReason = ""
	DegradationParseFailed       DegradationReason = "parse_failed"
	DegradationLowConfidence     DegradationReason = "low_confidence"
	DegradationMissingCapability DegradationReason = "missing_capability"
	DegradationPermissionScope   DegradationReason = "permission_scope"
	DegradationGraphNotNeeded    DegradationReason = "graph_not_needed"
)

type ComplexityFewShot struct {
	Question string           `json:"question" yaml:"question"`
	Level    ComplexityLevel  `json:"level" yaml:"level"`
	Subtype  ReasoningSubtype `json:"subtype" yaml:"subtype"`
}

type RoutingCapabilities struct {
	QuickRAG         bool `json:"quick_rag" yaml:"quick_rag"`
	ContextualRAG    bool `json:"contextual_rag" yaml:"contextual_rag"`
	GraphReasoning   bool `json:"graph_reasoning" yaml:"graph_reasoning"`
	VerifiedAgent    bool `json:"verified_agent" yaml:"verified_agent"`
	WebSearchEnabled bool `json:"web_search_enabled" yaml:"web_search_enabled"`
}

func (c RoutingCapabilities) Supports(action RoutingAction) bool {
	switch action {
	case RoutingQuickRAG:
		return c.QuickRAG
	case RoutingContextualRAG:
		return c.ContextualRAG
	case RoutingGraphReasoning:
		return c.GraphReasoning
	case RoutingVerifiedAgent:
		return c.VerifiedAgent
	default:
		return false
	}
}

type RoutingBudget struct {
	QueryExpansion      bool `json:"query_expansion"`
	RetrievalTopK       int  `json:"retrieval_top_k"`
	GraphEnabled        bool `json:"graph_enabled"`
	MaxAgentIterations  int  `json:"max_agent_iterations"`
	VerificationEnabled bool `json:"verification_enabled"`
}

type ComplexityRoutingConfig struct {
	Enabled                bool                              `json:"enabled" yaml:"enabled"`
	TaxonomyID             string                            `json:"taxonomy_id" yaml:"taxonomy_id"`
	TaxonomyVersion        string                            `json:"taxonomy_version" yaml:"taxonomy_version"`
	ConfidenceThreshold    float64                           `json:"confidence_threshold" yaml:"confidence_threshold"`
	FallbackAction         RoutingAction                     `json:"fallback_action" yaml:"fallback_action"`
	LevelActions           map[ComplexityLevel]RoutingAction `json:"level_actions" yaml:"level_actions"`
	DegradationChains      map[RoutingAction][]RoutingAction `json:"degradation_chains" yaml:"degradation_chains"`
	Capabilities           RoutingCapabilities               `json:"capabilities" yaml:"capabilities"`
	FewShot                []ComplexityFewShot               `json:"few_shot" yaml:"few_shot"`
	InputBudgetChars       int                               `json:"input_budget_chars" yaml:"input_budget_chars"`
	BudgetByAction         map[RoutingAction]RoutingBudget   `json:"budget_by_action" yaml:"budget_by_action"`
	confidenceThresholdSet bool
}

func (c *ComplexityRoutingConfig) UnmarshalJSON(data []byte) error {
	type configAlias ComplexityRoutingConfig
	var decoded configAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*c = ComplexityRoutingConfig(decoded)
	_, c.confidenceThresholdSet = fields["confidence_threshold"]
	return nil
}

func (c *ComplexityRoutingConfig) UnmarshalYAML(node *yaml.Node) error {
	type configAlias ComplexityRoutingConfig
	var decoded configAlias
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*c = ComplexityRoutingConfig(decoded)
	c.confidenceThresholdSet = yamlMappingHasKey(node, "confidence_threshold")
	return nil
}

func DefaultComplexityRoutingConfig() ComplexityRoutingConfig {
	return ComplexityRoutingConfig{
		Enabled: true, TaxonomyID: "question-complexity", TaxonomyVersion: "1.0", ConfidenceThreshold: .60,
		FallbackAction: RoutingContextualRAG,
		LevelActions: map[ComplexityLevel]RoutingAction{
			ComplexityL1: RoutingQuickRAG, ComplexityL2: RoutingContextualRAG,
			ComplexityL3: RoutingGraphReasoning, ComplexityL4: RoutingVerifiedAgent,
		},
		DegradationChains: map[RoutingAction][]RoutingAction{
			RoutingQuickRAG:       {RoutingQuickRAG},
			RoutingContextualRAG:  {RoutingContextualRAG, RoutingQuickRAG},
			RoutingGraphReasoning: {RoutingGraphReasoning, RoutingContextualRAG, RoutingQuickRAG},
			RoutingVerifiedAgent:  {RoutingVerifiedAgent, RoutingGraphReasoning, RoutingContextualRAG, RoutingQuickRAG},
		},
		// Research stack defaults: all routing actions available; degrade only when truly missing.
		Capabilities: RoutingCapabilities{
			QuickRAG: true, ContextualRAG: true, GraphReasoning: true, VerifiedAgent: true,
		},
		InputBudgetChars: 12000,
		BudgetByAction: map[RoutingAction]RoutingBudget{
			RoutingQuickRAG:       {RetrievalTopK: 5},
			RoutingContextualRAG:  {QueryExpansion: true, RetrievalTopK: 10},
			RoutingGraphReasoning: {QueryExpansion: true, RetrievalTopK: 12, GraphEnabled: true},
			RoutingVerifiedAgent:  {QueryExpansion: true, RetrievalTopK: 12, GraphEnabled: true, MaxAgentIterations: 6, VerificationEnabled: true},
		},
	}
}

func (c *ComplexityRoutingConfig) EnsureDefaults() {
	if c == nil {
		return
	}
	d := DefaultComplexityRoutingConfig()
	if c.TaxonomyID == "" {
		c.TaxonomyID = d.TaxonomyID
	}
	if c.TaxonomyVersion == "" {
		c.TaxonomyVersion = d.TaxonomyVersion
	}
	if c.ConfidenceThreshold == 0 && !c.confidenceThresholdSet {
		c.ConfidenceThreshold = d.ConfidenceThreshold
	}
	if c.FallbackAction == "" {
		c.FallbackAction = d.FallbackAction
	}
	if c.LevelActions == nil {
		c.LevelActions = d.LevelActions
	}
	if c.DegradationChains == nil {
		c.DegradationChains = d.DegradationChains
	}
	if c.InputBudgetChars == 0 {
		c.InputBudgetChars = d.InputBudgetChars
	}
	if c.BudgetByAction == nil {
		c.BudgetByAction = d.BudgetByAction
	}
	// Zero capabilities would make every action fail Supports(); fill research defaults.
	if !c.Capabilities.QuickRAG && !c.Capabilities.ContextualRAG &&
		!c.Capabilities.GraphReasoning && !c.Capabilities.VerifiedAgent {
		c.Capabilities = d.Capabilities
	}
}

func (c ComplexityRoutingConfig) Validate() error {
	if c.ConfidenceThreshold < 0 || c.ConfidenceThreshold > 1 || math.IsNaN(c.ConfidenceThreshold) {
		return fmt.Errorf("confidence_threshold must be between 0 and 1")
	}
	if c.InputBudgetChars < 0 {
		return fmt.Errorf("input_budget_chars must not be negative")
	}
	if !validRoutingAction(c.FallbackAction) {
		return fmt.Errorf("unknown fallback action %q", c.FallbackAction)
	}
	for level, action := range c.LevelActions {
		if !validComplexityLevel(level) {
			return fmt.Errorf("unknown complexity level %q", level)
		}
		if !validRoutingAction(action) {
			return fmt.Errorf("unknown action %q for level %q", action, level)
		}
	}
	for target, chain := range c.DegradationChains {
		if !validRoutingAction(target) || len(chain) == 0 || chain[0] != target {
			return fmt.Errorf("degradation chain for %q must start with its target action", target)
		}
		seen := map[RoutingAction]bool{}
		lastRank := routingRank(target)
		for _, action := range chain {
			if !validRoutingAction(action) {
				return fmt.Errorf("unknown action %q in degradation chain for %q", action, target)
			}
			if seen[action] {
				return fmt.Errorf("duplicate action %q in degradation chain for %q", action, target)
			}
			seen[action] = true
			if routingRank(action) > lastRank {
				return fmt.Errorf("degradation chain for %q contains an upgrade to %q", target, action)
			}
			lastRank = routingRank(action)
		}
	}
	return nil
}

type QuestionComplexity struct {
	Level               ComplexityLevel  `json:"complexity_level"`
	Subtype             ReasoningSubtype `json:"reasoning_subtype"`
	NeedsEntityRelation bool             `json:"needs_entity_relation"`
	Confidence          float64          `json:"confidence"`
	RationaleSummary    string           `json:"rationale_summary,omitempty"`
}

func (q QuestionComplexity) Validate() error {
	if !validComplexityLevel(q.Level) {
		return fmt.Errorf("unknown complexity level %q", q.Level)
	}
	if !validSubtype(q.Subtype) {
		return fmt.Errorf("unknown reasoning subtype %q", q.Subtype)
	}
	if q.Confidence < 0 || q.Confidence > 1 || math.IsNaN(q.Confidence) {
		return fmt.Errorf("confidence must be between 0 and 1")
	}
	return nil
}

func ParseQuestionComplexity(raw string) (QuestionComplexity, error) {
	var value struct {
		Level               string  `json:"complexity_level"`
		Subtype             string  `json:"reasoning_subtype"`
		NeedsEntityRelation bool    `json:"needs_entity_relation"`
		Confidence          float64 `json:"confidence"`
		Rationale           string  `json:"rationale_summary"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &value); err != nil {
		return QuestionComplexity{}, fmt.Errorf("parse complexity JSON: %w", err)
	}
	q := QuestionComplexity{Level: ComplexityLevel(strings.TrimSpace(value.Level)), Subtype: ReasoningSubtype(strings.TrimSpace(value.Subtype)), NeedsEntityRelation: value.NeedsEntityRelation, Confidence: value.Confidence, RationaleSummary: truncateRationale(strings.TrimSpace(value.Rationale))}
	if err := q.Validate(); err != nil {
		return QuestionComplexity{}, err
	}
	return q, nil
}

type RoutingDecision struct {
	Classification       QuestionComplexity `json:"classification"`
	PlannedAction        RoutingAction      `json:"planned_action"`
	ActualAction         RoutingAction      `json:"actual_action"`
	DegradationReason    DegradationReason  `json:"degradation_reason,omitempty"`
	TaxonomyID           string             `json:"taxonomy_id"`
	TaxonomyVersion      string             `json:"taxonomy_version"`
	ClassificationMillis int64              `json:"classification_millis"`
	Budget               RoutingBudget      `json:"budget"`
}

func (d RoutingDecision) Summary() map[string]any {
	return map[string]any{"level": d.Classification.Level, "subtype": d.Classification.Subtype, "needs_entity_relation": d.Classification.NeedsEntityRelation, "confidence": d.Classification.Confidence, "planned_action": d.PlannedAction, "actual_action": d.ActualAction, "degradation_reason": d.DegradationReason, "taxonomy_id": d.TaxonomyID, "taxonomy_version": d.TaxonomyVersion, "classification_ms": d.ClassificationMillis}
}

func PlanRouting(complexity QuestionComplexity, cfg ComplexityRoutingConfig) RoutingDecision {
	cfg.EnsureDefaults()
	fallback := conservativeRoutingFallback(cfg.FallbackAction)
	d := RoutingDecision{Classification: complexity, TaxonomyID: cfg.TaxonomyID, TaxonomyVersion: cfg.TaxonomyVersion, PlannedAction: fallback, ActualAction: fallback}
	if err := complexity.Validate(); err != nil {
		d.DegradationReason, d.Classification = DegradationParseFailed, QuestionComplexity{Level: ComplexityL2, Subtype: SubtypeContextualFact}
	} else if complexity.Confidence < cfg.ConfidenceThreshold {
		d.DegradationReason = DegradationLowConfidence
	} else if action, ok := cfg.LevelActions[complexity.Level]; ok {
		d.PlannedAction, d.ActualAction = action, action
	}
	for _, action := range cfg.DegradationChains[d.PlannedAction] {
		if action == RoutingGraphReasoning && !d.Classification.NeedsEntityRelation {
			if d.DegradationReason == "" {
				d.DegradationReason = DegradationGraphNotNeeded
			}
			continue
		}
		if cfg.Capabilities.Supports(action) {
			d.ActualAction, d.Budget = action, cfg.BudgetByAction[action]
			if !d.Classification.NeedsEntityRelation {
				d.Budget.GraphEnabled = false
			}
			if action != d.PlannedAction && d.DegradationReason == "" {
				d.DegradationReason = DegradationMissingCapability
			}
			return d
		}
	}
	d.ActualAction, d.DegradationReason, d.Budget = RoutingQuickRAG, DegradationMissingCapability, cfg.BudgetByAction[RoutingQuickRAG]
	return d
}

func conservativeRoutingFallback(action RoutingAction) RoutingAction {
	if routingRank(action) > routingRank(RoutingContextualRAG) || !validRoutingAction(action) {
		return RoutingContextualRAG
	}
	return action
}

func validComplexityLevel(level ComplexityLevel) bool {
	return level == ComplexityL1 || level == ComplexityL2 || level == ComplexityL3 || level == ComplexityL4
}
func validSubtype(subtype ReasoningSubtype) bool {
	switch subtype {
	case SubtypeExplicitFact, SubtypeContextualFact, SubtypeComparison, SubtypeMultiHop, SubtypeCausal, SubtypeHypothetical, SubtypeTransfer, SubtypeUnknown:
		return true
	default:
		return false
	}
}
func validRoutingAction(action RoutingAction) bool {
	return action == RoutingQuickRAG || action == RoutingContextualRAG || action == RoutingGraphReasoning || action == RoutingVerifiedAgent
}
func routingRank(action RoutingAction) int {
	switch action {
	case RoutingQuickRAG:
		return 1
	case RoutingContextualRAG:
		return 2
	case RoutingGraphReasoning:
		return 3
	case RoutingVerifiedAgent:
		return 4
	default:
		return 0
	}
}
func truncateRationale(value string) string {
	r := []rune(value)
	if len(r) > 240 {
		return string(r[:240])
	}
	return value
}

func SortedActions(actions map[RoutingAction]RoutingBudget) []RoutingAction {
	result := make([]RoutingAction, 0, len(actions))
	for action := range actions {
		result = append(result, action)
	}
	sort.Slice(result, func(i, j int) bool { return routingRank(result[i]) < routingRank(result[j]) })
	return result
}
