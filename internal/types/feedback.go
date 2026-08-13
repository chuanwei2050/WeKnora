package types

import (
	"fmt"
	"time"
)

type FeedbackStatus string

const (
	FeedbackPending  FeedbackStatus = "pending"
	FeedbackAccepted FeedbackStatus = "accepted"
	FeedbackRejected FeedbackStatus = "rejected"
)

type FeedbackTarget string

const (
	FeedbackTargetKnowledgeDraft    FeedbackTarget = "knowledge_draft"
	FeedbackTargetEvaluationCase    FeedbackTarget = "evaluation_case"
	FeedbackTargetImprovementTicket FeedbackTarget = "improvement_ticket"
)

type AnswerFeedback struct {
	ID            string         `json:"id"`
	TenantID      uint64         `json:"tenant_id"`
	SessionID     string         `json:"session_id"`
	MessageID     string         `json:"message_id"`
	AnswerVersion string         `json:"answer_version"`
	Rating        int            `json:"rating"`
	Correction    string         `json:"correction,omitempty"`
	Target        FeedbackTarget `json:"target,omitempty"`
	Status        FeedbackStatus `json:"status"`
	ReviewerID    string         `json:"reviewer_id,omitempty"`
	CandidateID   string         `json:"candidate_id,omitempty"`
	CreatedAt     time.Time      `json:"created_at,omitempty"`
}

func (AnswerFeedback) TableName() string { return "answer_feedback" }

type FeedbackCandidateStatus string

const (
	FeedbackCandidatePendingReview FeedbackCandidateStatus = "pending_review"
	FeedbackCandidateRejected      FeedbackCandidateStatus = "rejected"
)

type FeedbackCandidate struct {
	ID         string                  `json:"id"`
	TenantID   uint64                  `json:"tenant_id"`
	FeedbackID string                  `json:"feedback_id"`
	Target     FeedbackTarget          `json:"target"`
	Status     FeedbackCandidateStatus `json:"status"`
	Payload    map[string]interface{}  `json:"payload"`
	CreatedAt  time.Time               `json:"created_at"`
}

type FeedbackCapabilities struct {
	KnowledgeGovernance bool
	AcceptanceBenchmark bool
	ImprovementTicket   bool
}

func ValidateFeedback(f AnswerFeedback) error {
	if f.TenantID == 0 || f.SessionID == "" || f.MessageID == "" || f.AnswerVersion == "" {
		return fmt.Errorf("feedback must identify tenant, session, message and answer version")
	}
	if f.Rating < 1 || f.Rating > 5 {
		return fmt.Errorf("rating must be between 1 and 5")
	}
	if f.Status != "" && f.Status != FeedbackPending && f.Status != FeedbackAccepted && f.Status != FeedbackRejected {
		return fmt.Errorf("unknown feedback status")
	}
	return nil
}

func ValidateFeedbackAdoption(status FeedbackStatus, target FeedbackTarget, capabilities FeedbackCapabilities) error {
	if status != FeedbackPending {
		return fmt.Errorf("only pending feedback can be adopted")
	}
	switch target {
	case FeedbackTargetKnowledgeDraft:
		if !capabilities.KnowledgeGovernance {
			return fmt.Errorf("knowledge governance is unavailable")
		}
	case FeedbackTargetEvaluationCase:
		if !capabilities.AcceptanceBenchmark {
			return fmt.Errorf("acceptance benchmark is unavailable")
		}
	case FeedbackTargetImprovementTicket:
		if !capabilities.ImprovementTicket {
			return fmt.Errorf("improvement ticket capability is unavailable")
		}
	default:
		return fmt.Errorf("unknown feedback target")
	}
	return nil
}

func ValidateFeedbackTarget(target FeedbackTarget) error {
	switch target {
	case FeedbackTargetKnowledgeDraft, FeedbackTargetEvaluationCase, FeedbackTargetImprovementTicket:
		return nil
	default:
		return fmt.Errorf("unknown feedback target")
	}
}
