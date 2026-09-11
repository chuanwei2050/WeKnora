package types

import "time"

// Graph rebuild job phases for KB settings progress UI.
const (
	GraphRebuildIdle           = "idle"
	GraphRebuildRunning        = "running"
	GraphRebuildAwaitingReview = "awaiting_review"
	GraphRebuildCompleted      = "completed"
	GraphRebuildFailed         = "failed"
)

// GraphRebuildProgress is persisted in Redis for rebuild-graph UX.
type GraphRebuildProgress struct {
	Status        string     `json:"status"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	Total         int        `json:"total"`
	Processed     int        `json:"processed"`
	Percent       int        `json:"percent"`
	RequireReview bool       `json:"require_review"`
	PendingReview int64      `json:"pending_review"`
	DocumentCount int        `json:"document_count"`
	Message       string     `json:"message,omitempty"`
}
