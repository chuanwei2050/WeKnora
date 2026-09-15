package types

import "time"

type KBMaintenanceOperation string

const (
	KBMaintenanceReparse    KBMaintenanceOperation = "reparse"
	KBMaintenanceRechunk    KBMaintenanceOperation = "rechunk"
	KBMaintenanceGraph      KBMaintenanceOperation = "graph"
	KBMaintenanceKeywords   KBMaintenanceOperation = "keywords"
	KBMaintenanceVector     KBMaintenanceOperation = "vector"
	KBMaintenanceQuestions  KBMaintenanceOperation = "questions"
	KBMaintenanceStructured KBMaintenanceOperation = "structured"
)

// IsDocumentPipelineMaintenance reports whether the operation drives per-document
// parse/reparse work and must keep the maintenance lock until in-flight documents finish.
func (op KBMaintenanceOperation) IsDocumentPipelineMaintenance() bool {
	return op == KBMaintenanceReparse || op == KBMaintenanceRechunk
}

// ParseInterruptedForRechunkMessage is written onto stuck pending/processing
// documents so leftover queue workers can recognize they were superseded.
const ParseInterruptedForRechunkMessage = "解析已中断，准备重新解析分块"

// ParseInterruptedForRebuildMessage is written when full rebuild interrupts
// orphaned pending/processing documents before re-enqueueing them.
const ParseInterruptedForRebuildMessage = "解析已中断，准备全部重建"

// ParseStaleInterruptedMessage is written when a processing document is orphaned
// (worker crash, lease expiry, or exhausted retries without a status write-back).
// Prefer MarkDocumentProcessFailed(cause) when the real error is known; this is
// only the fallback for silent orphans (often multimodal/VLM timeouts).
const ParseStaleInterruptedMessage = "解析超时未完成（常见于多模态图片处理过久、VLM 超时或 worker 中断），请重试"

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
	// EnqueueDone is false while a document-pipeline job is still staging
	// ReparseKnowledge calls. Progress must not advance/finish until true,
	// otherwise targets still marked completed/failed look "done" too early.
	EnqueueDone bool `json:"enqueue_done,omitempty"`
}

type KBMaintenancePayload struct {
	TenantID        uint64                 `json:"tenant_id"`
	KnowledgeBaseID string                 `json:"knowledge_base_id"`
	RunID           string                 `json:"run_id"`
	Operation       KBMaintenanceOperation `json:"operation"`
}
