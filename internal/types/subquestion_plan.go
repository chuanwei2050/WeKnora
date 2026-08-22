package types

import (
	"fmt"
	"strings"
)

// SubQuestion is an ordered, bounded retrieval unit. It carries no model
// reasoning text; only the user-visible query and dependency index survive.
type SubQuestion struct {
	Index     int    `json:"index"`
	Query     string `json:"query"`
	DependsOn []int  `json:"depends_on,omitempty"`
	Required  bool   `json:"required"`
}

type SubQuestionPlan struct {
	Questions     []SubQuestion `json:"questions"`
	MaxQuestions  int           `json:"max_questions"`
	MaxModelCalls int           `json:"max_model_calls"`
	MaxDurationMs int64         `json:"max_duration_ms"`
}

func (p SubQuestionPlan) Validate() error {
	if p.MaxQuestions < 1 || p.MaxQuestions > 8 || len(p.Questions) > p.MaxQuestions {
		return fmt.Errorf("sub-question count exceeds bound")
	}
	if p.MaxModelCalls < 1 || p.MaxModelCalls > 16 {
		return fmt.Errorf("sub-question model-call budget is invalid")
	}
	if p.MaxDurationMs < 1 || p.MaxDurationMs > 120000 {
		return fmt.Errorf("sub-question duration budget is invalid")
	}
	seen := map[int]bool{}
	for _, question := range p.Questions {
		if question.Index != len(seen)+1 || seen[question.Index] || strings.TrimSpace(question.Query) == "" {
			return fmt.Errorf("sub-question index and query are invalid")
		}
		seen[question.Index] = true
		for _, dependency := range question.DependsOn {
			if dependency >= question.Index || !seen[dependency] {
				return fmt.Errorf("sub-question dependencies must refer to prior questions")
			}
		}
	}
	return nil
}

// ParseSubQuestionPlan parses the strict model-produced sub-question
// contract. Dependencies must point to earlier questions only.
func ParseSubQuestionPlan(raw, originalQuery string, maxQuestions, maxCalls int, maxDurationMs int64) (SubQuestionPlan, error) {
	var payload struct {
		Questions []SubQuestion `json:"questions"`
	}
	if err := decodeStrictJSON(raw, &payload); err != nil {
		return SubQuestionPlan{}, fmt.Errorf("parse sub-question plan: %w", err)
	}
	if strings.TrimSpace(originalQuery) == "" || len(payload.Questions) == 0 {
		return SubQuestionPlan{}, fmt.Errorf("original query and at least one sub-question are required")
	}
	plan := SubQuestionPlan{Questions: payload.Questions, MaxQuestions: maxQuestions, MaxModelCalls: maxCalls, MaxDurationMs: maxDurationMs}
	if err := plan.Validate(); err != nil {
		return SubQuestionPlan{}, err
	}
	return plan, nil
}

// PlanSubQuestions creates a conservative one-step fallback plan. It is used
// when decomposition is unavailable; it must not pretend that a complex
// question was actually split.
func PlanSubQuestions(query string, complexity QuestionComplexity, maxQuestions, maxCalls int, maxDurationMs int64) (SubQuestionPlan, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return SubQuestionPlan{}, fmt.Errorf("query is required")
	}
	if maxQuestions == 0 {
		maxQuestions = 4
	}
	if maxCalls == 0 {
		maxCalls = maxQuestions
	}
	if maxDurationMs == 0 {
		maxDurationMs = 30000
	}
	_ = complexity
	questions := []SubQuestion{{Index: 1, Query: query, Required: true}}
	plan := SubQuestionPlan{Questions: questions, MaxQuestions: maxQuestions, MaxModelCalls: maxCalls, MaxDurationMs: maxDurationMs}
	return plan, plan.Validate()
}
