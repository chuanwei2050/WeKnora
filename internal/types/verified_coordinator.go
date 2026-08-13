package types

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// VerifiedAnswerCoordinator is the pure, dependency-free orchestration
// contract used by both the chat pipeline and service tests.
type VerifiedAnswerCoordinator struct {
	Config VerifiedAnswerConfig
}

func NewVerifiedAnswerCoordinator(config VerifiedAnswerConfig) *VerifiedAnswerCoordinator {
	config.EnsureDefaults()
	return &VerifiedAnswerCoordinator{Config: config}
}

func (c *VerifiedAnswerCoordinator) Execute(ctx context.Context, query string, identities []ModelIdentity, hooks VerificationHooks) (*VerifiedAnswer, error) {
	if c == nil || !c.Config.Enabled {
		return nil, fmt.Errorf("verified answering is disabled")
	}
	if hooks.Retrieve == nil || hooks.Draft == nil || (hooks.Validate == nil && hooks.ValidateMany == nil) {
		return nil, fmt.Errorf("verified answering hooks are incomplete")
	}
	if err := c.Config.Validate(identities); err != nil {
		return nil, err
	}
	workCtx := ctx
	if c.Config.Budget.MaxWallClockMillis > 0 {
		var cancel context.CancelFunc
		workCtx, cancel = context.WithTimeout(ctx, time.Duration(c.Config.Budget.MaxWallClockMillis)*time.Millisecond)
		defer cancel()
	}
	ledger := NewVerificationBudgetLedger(c.Config.Budget)
	if err := ledger.Reserve(1, hooks.InitialUsage.PromptTokens, hooks.InitialUsage.CompletionTokens, 0); err != nil {
		return c.conservative(DraftAnswer{}, EvidenceBundle{}, err)
	}
	evidence, err := hooks.Retrieve(workCtx, query)
	if err != nil {
		return c.conservative(DraftAnswer{}, evidence, err)
	}
	if hooks.Scope != nil {
		if err := hooks.Scope.ValidateBundle(evidence); err != nil {
			return c.conservative(DraftAnswer{}, evidence, err)
		}
	}
	draft, err := hooks.Draft(workCtx, query, evidence)
	if err != nil {
		return c.conservative(DraftAnswer{}, evidence, err)
	}
	answer := &VerifiedAnswer{Evidence: evidence.Items, RetrievalCount: 1}
	if hooks.RoutingDecision != nil {
		answer.ExecutionPath = verificationExecutionPath(hooks.RoutingDecision.ActualAction)
	}
	for round := 0; ; round++ {
		if err := workCtx.Err(); err != nil {
			return c.conservativeFromAnswer(answer, draft, evidence, err)
		}
		calls := 1
		estimate := VerificationBudgetEstimate{}
		if hooks.EstimateValidationBudget != nil {
			estimate = hooks.EstimateValidationBudget(draft, evidence)
		}
		if hooks.ValidateMany != nil {
			calls = len(identities)
			if calls < 1 {
				calls = 1
			}
		}
		parallelCalls := 0
		if hooks.ValidateMany != nil && calls > 0 {
			parallelCalls = calls
		}
		if hooks.EstimateValidationBudget == nil {
			estimate.ModelCalls = calls
		}
		if err := ledger.Reserve(estimate.ModelCalls, estimate.InputTokens*calls, estimate.OutputTokens*calls, parallelCalls); err != nil {
			return c.conservativeFromAnswer(answer, draft, evidence, err)
		}
		var reports []ValidationReport
		if hooks.ValidateMany != nil {
			reports, err = hooks.ValidateMany(workCtx, draft, evidence)
			ledger.ReleaseParallelCalls(parallelCalls)
		} else {
			var report ValidationReport
			report, err = hooks.Validate(workCtx, draft, evidence)
			reports = []ValidationReport{report}
		}
		if err != nil {
			return c.conservativeFromAnswer(answer, draft, evidence, err)
		}
		for index := range reports {
			if reports[index].ID == "" {
				reports[index].ID = uuid.NewString()
			}
			if reports[index].Degraded {
				answer.Degraded = true
				if answer.ConservativeNote == "" {
					answer.ConservativeNote = "部分验证能力不可用，请人工核验。"
				}
			}
		}
		answer.Reports = append(answer.Reports, reports...)
		// Keep every round in the audit trail, but make the decision from the
		// current validator batch. A successful reflection must be able to
		// replace a failed draft instead of being averaged with stale scores.
		score, decision, aggregateErr := AggregateValidation(reports, c.Config.Weights)
		if aggregateErr != nil {
			return c.conservativeFromAnswer(answer, draft, evidence, aggregateErr)
		}
		answer.Confidence = score
		if decision == VerificationPassed {
			answer.Text, answer.Decision = draft.Text, VerificationPassed
			return answer, nil
		}
		if round >= c.Config.MaxReflections {
			return c.conservativeFromAnswer(answer, draft, evidence, fmt.Errorf("reflection budget exhausted"))
		}
		plan := ReflectionPlan{Action: ReflectionRewrite, Round: round + 1}
		if hooks.Reflect != nil {
			estimate := VerificationBudgetEstimate{}
			if hooks.EstimateReflectionBudget != nil {
				estimate = hooks.EstimateReflectionBudget(draft, evidence, answer.Reports)
			}
			if estimate.ModelCalls > 0 {
				if err := ledger.Reserve(estimate.ModelCalls, estimate.InputTokens, estimate.OutputTokens, 0); err != nil {
					return c.conservativeFromAnswer(answer, draft, evidence, err)
				}
			}
			plan, err = hooks.Reflect(workCtx, draft, answer.Reports)
			if err != nil {
				return c.conservativeFromAnswer(answer, draft, evidence, err)
			}
		}
		switch plan.Action {
		case ReflectionStop:
			answer.ReflectionActions = append(answer.ReflectionActions, string(ReflectionStop))
			return c.conservativeFromAnswer(answer, draft, evidence, fmt.Errorf("reflection stopped: %s", plan.Reason))
		case ReflectionRetrieveMore:
			answer.ReflectionActions = append(answer.ReflectionActions, string(ReflectionRetrieveMore))
			var more EvidenceBundle
			var retrieveErr error
			if hooks.RetrieveMore != nil {
				retrievalRequest := RetrievalRequest{
					Query: query, Reason: plan.Reason, Round: plan.Round,
					Scope: verificationScopeSnapshot(hooks.Scope), RoutingDecision: hooks.RoutingDecision,
				}
				if hooks.EstimateRetrievalBudget != nil {
					retrievalEstimate := hooks.EstimateRetrievalBudget(retrievalRequest)
					if err := ledger.Reserve(retrievalEstimate.ModelCalls, retrievalEstimate.InputTokens, retrievalEstimate.OutputTokens, 0); err != nil {
						return c.conservativeFromAnswer(answer, draft, evidence, err)
					}
				}
				more, retrieveErr = hooks.RetrieveMore(workCtx, retrievalRequest)
			} else {
				// Keep the old hook usable for pure coordinator callers. Production
				// integrations must provide RetrieveMore so the request carries scope.
				more, retrieveErr = hooks.Retrieve(workCtx, query)
			}
			if retrieveErr != nil {
				return c.conservativeFromAnswer(answer, draft, evidence, retrieveErr)
			}
			if hooks.Scope != nil {
				if scopeErr := hooks.Scope.ValidateBundle(more); scopeErr != nil {
					return c.conservativeFromAnswer(answer, draft, evidence, scopeErr)
				}
			}
			before := len(evidence.Items)
			evidence = mergeEvidenceBundles(evidence, more)
			if hooks.RetrieveMore != nil && len(evidence.Items) == before {
				answer.ReflectionActions = append(answer.ReflectionActions, "no_new_evidence")
				conservative, _ := c.conservative(draft, evidence, fmt.Errorf("no_new_evidence"))
				conservative.ExecutionPath = answer.ExecutionPath
				conservative.ReflectionActions = append([]string(nil), answer.ReflectionActions...)
				conservative.RetrievalCount = answer.RetrievalCount
				return conservative, nil
			}
			answer.RetrievalCount++
		case ReflectionRewrite:
			answer.ReflectionActions = append(answer.ReflectionActions, string(ReflectionRewrite))
		default:
			return c.conservativeFromAnswer(answer, draft, evidence, fmt.Errorf("unknown reflection action %q", plan.Action))
		}
		draft, err = hooks.Draft(workCtx, query, evidence)
		if err != nil {
			return c.conservativeFromAnswer(answer, draft, evidence, err)
		}
	}
}

func verificationScopeSnapshot(scope *VerificationScope) VerificationScope {
	if scope == nil {
		return VerificationScope{}
	}
	copy := *scope
	copy.KnowledgeBaseIDs = append([]string(nil), scope.KnowledgeBaseIDs...)
	copy.KnowledgeIDs = append([]string(nil), scope.KnowledgeIDs...)
	return copy
}

func verificationExecutionPath(action RoutingAction) string {
	if action == RoutingVerifiedAgent {
		return "verified_agent"
	}
	return "verified_rag_postcheck"
}

func (c *VerifiedAnswerCoordinator) conservative(draft DraftAnswer, evidence EvidenceBundle, cause error) (*VerifiedAnswer, error) {
	answer := &VerifiedAnswer{Decision: VerificationConservative, Degraded: true, Evidence: evidence.Items, RetrievalCount: boolToInt(len(evidence.Items) > 0), ConservativeNote: "当前知识或验证预算不足，以下内容仅供核验参考。"}
	if draft.Text != "" {
		answer.Text = draft.Text
	}
	if cause != nil {
		answer.ConservativeNote += " " + cause.Error()
	}
	return answer, nil
}

func (c *VerifiedAnswerCoordinator) conservativeFromAnswer(previous *VerifiedAnswer, draft DraftAnswer, evidence EvidenceBundle, cause error) (*VerifiedAnswer, error) {
	answer, _ := c.conservative(draft, evidence, cause)
	if previous == nil {
		return answer, nil
	}
	answer.Confidence = previous.Confidence
	answer.ExecutionPath = previous.ExecutionPath
	answer.ReflectionActions = append([]string(nil), previous.ReflectionActions...)
	answer.RetrievalCount = previous.RetrievalCount
	answer.Reports = append([]ValidationReport(nil), previous.Reports...)
	return answer, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func mergeEvidenceBundles(left, right EvidenceBundle) EvidenceBundle {
	seen := make(map[string]bool, len(left.Items)+len(right.Items))
	result := left
	for _, evidence := range left.Items {
		seen[evidence.ID] = true
	}
	for _, evidence := range right.Items {
		if evidence.ID != "" && !seen[evidence.ID] {
			seen[evidence.ID] = true
			result.Items = append(result.Items, evidence)
		}
	}
	return result
}
