package types

import "time"

type KBMaintenanceOperation string

const (
	KBMaintenanceReparse    KBMaintenanceOperation = "reparse"
	KBMaintenanceGraph      KBMaintenanceOperation = "graph"
	KBMaintenanceKeywords   KBMaintenanceOperation = "keywords"
	KBMaintenanceVector     KBMaintenanceOperation = "vector"
	KBMaintenanceQuestions  KBMaintenanceOperation = "questions"
	KBMaintenanceStructured KBMaintenanceOperation = "structured"
)

type KBMaintenanceProgress struct {
	RunID              string                 `json:"run_id"`
	Operation          KBMaintenanceOperation `json:"operation"`
	Status             string                 `json:"status"`
	Total              int                    `json:"total"`
	Processed          int                    `json:"processed"`
	Failed             int                    `json:"failed"`
	Percent            int                    `json:"percent"`
	Message            string                 `json:"message,omitempty"`
	StartedAt          time.Time              `json:"started_at"`
	FinishedAt         *time.Time             `json:"finished_at,omitempty"`
	HeartbeatAt        time.Time              `json:"heartbeat_at"`
	TargetKnowledgeIDs []string               `json:"target_knowledge_ids,omitempty"`
}

type KBMaintenancePayload struct {
	TenantID        uint64                 `json:"tenant_id"`
	KnowledgeBaseID string                 `json:"knowledge_base_id"`
	RunID           string                 `json:"run_id"`
	Operation       KBMaintenanceOperation `json:"operation"`
}
