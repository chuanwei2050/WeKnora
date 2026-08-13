package types

import (
	"fmt"

	"github.com/google/uuid"
)

// ValidateDraftAnswer performs the boundary checks that every validator must
// agree on before a model-specific judgement is accepted. It intentionally
// evaluates only evidence bindings and answer structure; semantic entailment
// remains the responsibility of a model validator.
func ValidateDraftAnswer(draft DraftAnswer, bundle EvidenceBundle, model ModelIdentity) ValidationReport {
	report := ValidationReport{ID: uuid.NewString(), Model: model}
	if err := draft.Validate(bundle); err != nil {
		report.Issues = append(report.Issues, ValidationIssue{
			Dimension: ValidationCompleteness,
			Severity:  SeverityCritical,
			Message:   err.Error(),
		})
		report.CitationScore = 0
		return report
	}
	if len(draft.Claims) == 0 {
		report.Issues = append(report.Issues, ValidationIssue{
			Dimension: ValidationCompleteness,
			Severity:  SeverityWarning,
			Message:   "draft contains no structured claims",
		})
		report.CompletenessScore = 0
	} else {
		report.CompletenessScore = 1
	}

	supportedClaims := 0
	for _, claim := range draft.Claims {
		if len(claim.EvidenceIDs) == 0 {
			report.Issues = append(report.Issues, ValidationIssue{
				ClaimID:   claim.ID,
				Dimension: ValidationCitation,
				Severity:  severityForClaim(claim.Core),
				Message:   "claim has no supporting evidence",
			})
			continue
		}
		supportedClaims++
	}
	if len(draft.Claims) > 0 {
		report.CitationScore = float64(supportedClaims) / float64(len(draft.Claims))
		report.FactScore = report.CitationScore
		report.LogicScore = 1
	} else {
		report.FactScore, report.LogicScore, report.CitationScore = 0, 0, 0
	}
	if report.CitationScore < 1 {
		report.CompletenessScore = report.CitationScore
	}
	return report
}

func severityForClaim(core bool) ValidationSeverity {
	if core {
		return SeverityCritical
	}
	return SeverityWarning
}

// ParseReflectionPlan accepts only the versioned action envelope. Reflection
// output is a control decision, so prose and unknown fields must fail closed.
func ParseReflectionPlan(raw string) (ReflectionPlan, error) {
	var plan ReflectionPlan
	if err := decodeStrictJSON(raw, &plan); err != nil {
		return ReflectionPlan{}, fmt.Errorf("parse reflection plan: %w", err)
	}
	switch plan.Action {
	case ReflectionRetrieveMore, ReflectionRewrite, ReflectionStop:
	default:
		return ReflectionPlan{}, fmt.Errorf("unknown reflection action %q", plan.Action)
	}
	if plan.Round < 0 {
		return ReflectionPlan{}, fmt.Errorf("reflection round must not be negative")
	}
	return plan, nil
}
