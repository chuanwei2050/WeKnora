package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/tracing/langfuse"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// ShouldEnqueueGraphExtract reports whether a chunk should enqueue graph extraction
// for the given knowledge base. Graph must be enabled and the chunk must show relation signals.
func ShouldEnqueueGraphExtract(kb *types.KnowledgeBase, chunkContent string) bool {
	if kb == nil || !kb.IsGraphEnabled() {
		return false
	}
	if strings.TrimSpace(chunkContent) == "" {
		return false
	}
	if kb.ExtractConfig != nil && kb.ExtractConfig.IngestionMode.Normalize() == types.GraphIngestionSignal {
		return types.NeedsEntityRelation(chunkContent)
	}
	return true
}

// KnowledgePostProcessService acts as an orchestrator for all post-processing tasks
// after a document has been parsed and split into chunks (including multimodal OCR/Caption).
type KnowledgePostProcessService struct {
	knowledgeRepo interfaces.KnowledgeRepository
	kbService     interfaces.KnowledgeBaseService
	chunkService  interfaces.ChunkService
	taskEnqueuer  interfaces.TaskEnqueuer
	redisClient   *redis.Client
}

func NewKnowledgePostProcessService(
	knowledgeRepo interfaces.KnowledgeRepository,
	kbService interfaces.KnowledgeBaseService,
	chunkService interfaces.ChunkService,
	taskEnqueuer interfaces.TaskEnqueuer,
	redisClient *redis.Client,
) interfaces.TaskHandler {
	return &KnowledgePostProcessService{
		knowledgeRepo: knowledgeRepo,
		kbService:     kbService,
		chunkService:  chunkService,
		taskEnqueuer:  taskEnqueuer,
		redisClient:   redisClient,
	}
}

// Handle implements asynq handler for TypeKnowledgePostProcess.
func (s *KnowledgePostProcessService) Handle(ctx context.Context, task *asynq.Task) error {
	var payload types.KnowledgePostProcessPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal knowledge post process payload: %w", err)
	}

	logger.Infof(ctx, "[KnowledgePostProcess] Orchestrating post processing for knowledge: %s", payload.KnowledgeID)

	ctx = context.WithValue(ctx, types.TenantIDContextKey, payload.TenantID)
	if payload.Language != "" {
		ctx = context.WithValue(ctx, types.LanguageContextKey, payload.Language)
	}

	// 1. Fetch Knowledge and KB
	knowledge, err := s.knowledgeRepo.GetKnowledgeByIDOnly(ctx, payload.KnowledgeID)
	if err != nil {
		return fmt.Errorf("get knowledge %s: %w", payload.KnowledgeID, err)
	}
	if knowledge == nil {
		logger.Warnf(ctx, "[KnowledgePostProcess] Knowledge %s not found, aborting.", payload.KnowledgeID)
		return nil
	}
	versionID := strings.TrimSpace(payload.VersionID)
	if versionID != "" && !governedVersionCanUpdateKnowledge(knowledge, versionID) {
		logger.Infof(ctx, "[KnowledgePostProcess] Skipping stale governed version %s for knowledge %s", versionID, payload.KnowledgeID)
		return nil
	}

	kb, err := s.kbService.GetKnowledgeBaseByIDOnly(ctx, payload.KnowledgeBaseID)
	if err != nil || kb == nil {
		return fmt.Errorf("get knowledge base %s: %w", payload.KnowledgeBaseID, err)
	}
	if versionID == "" && kb.Governance.Enabled {
		if strings.TrimSpace(knowledge.PendingVersionID) != "" {
			logger.Infof(ctx, "[KnowledgePostProcess] Skipping unbound governed task for knowledge %s", payload.KnowledgeID)
			return nil
		}
		versionID = strings.TrimSpace(knowledge.CurrentVersionID)
	}
	payload.VersionID = versionID

	// 2. Fetch all chunks
	chunks, err := s.chunkService.ListChunksByKnowledgeID(ctx, payload.KnowledgeID)
	if err != nil {
		return fmt.Errorf("list chunks for knowledge %s: %w", payload.KnowledgeID, err)
	}
	if versionID != "" {
		versionChunks := make([]*types.Chunk, 0, len(chunks))
		for _, chunk := range chunks {
			if chunk != nil && chunk.KnowledgeVersionID == versionID {
				versionChunks = append(versionChunks, chunk)
			}
		}
		chunks = versionChunks
	}

	// Gather all text-like chunks (including newly added OCR and Caption from multimodal tasks)
	var textChunks []*types.Chunk
	for _, c := range chunks {
		if c.ChunkType == types.ChunkTypeText || c.ChunkType == types.ChunkTypeImageOCR || c.ChunkType == types.ChunkTypeImageCaption {
			textChunks = append(textChunks, c)
		}
	}

	// 3. Update ParseStatus to Completed
	// (Except if it's already completed or if it was marked as failed/deleting, but we'll just set it to completed if it's processing)
	if knowledge.ParseStatus == types.ParseStatusProcessing {
		knowledge.ParseStatus = types.ParseStatusCompleted
		knowledge.ErrorMessage = ""
		knowledge.UpdatedAt = time.Now()

		if shouldGenerateDocumentSummary(textChunks) {
			knowledge.SummaryStatus = types.SummaryStatusPending
		} else {
			knowledge.SummaryStatus = types.SummaryStatusNone
		}

		values := map[string]any{
			"parse_status":   knowledge.ParseStatus,
			"error_message":  knowledge.ErrorMessage,
			"summary_status": knowledge.SummaryStatus,
			"updated_at":     knowledge.UpdatedAt,
		}
		updated, updateErr := updateKnowledgeForCurrentOrPendingVersion(ctx, s.knowledgeRepo, knowledge, versionID, values)
		if updateErr != nil || !updated {
			if updateErr != nil {
				logger.Warnf(ctx, "[KnowledgePostProcess] Failed to update knowledge status to completed: %v", updateErr)
			} else {
				logger.Infof(ctx, "[KnowledgePostProcess] Skipped stale governed version %s", versionID)
			}
			return nil
		}
		logger.Infof(ctx, "[KnowledgePostProcess] Knowledge %s marked as completed.", payload.KnowledgeID)
	}

	// 4. Spawn Summary and Question Tasks
	var enqueueErr error
	if shouldGenerateDocumentSummary(textChunks) {
		if err := s.enqueueSummaryGenerationTask(ctx, payload); err != nil {
			enqueueErr = err
		}
	}
	if len(textChunks) > 0 {
		// Question generation only makes sense for RAG indexing (improves chunk recall).
		// Skip when only Wiki/Graph is enabled without vector/keyword search.
		if kb.NeedsEmbeddingModel() {
			if err := s.enqueueQuestionGenerationIfEnabled(ctx, payload, kb); err != nil && enqueueErr == nil {
				enqueueErr = err
			}
		}
	}

	// 5. Spawn Graph RAG Tasks — only when graph indexing is enabled in IndexingStrategy
	if kb.IsGraphEnabled() {
		graphCandidates := 0
		for _, chunk := range textChunks {
			if !ShouldEnqueueGraphExtract(kb, chunk.Content) {
				continue
			}
			graphCandidates++
			err := NewChunkExtractTask(ctx, s.taskEnqueuer, payload.TenantID, chunk.ID, kb.GraphExtractionModelID())
			if err != nil {
				logger.Errorf(ctx, "[KnowledgePostProcess] Failed to create chunk extract task for %s: %v", chunk.ID, err)
				if enqueueErr == nil {
					enqueueErr = fmt.Errorf("enqueue graph extraction for chunk %s: %w", chunk.ID, err)
				}
			}
		}
		logger.Infof(ctx, "[KnowledgePostProcess] Spawning Graph RAG extract tasks for %d/%d eligible text-like chunks", graphCandidates, len(textChunks))
	}

	// 6. Spawn Wiki Ingest Task if wiki indexing is enabled in IndexingStrategy
	if kb.IndexingStrategy.WikiEnabled && len(textChunks) > 0 && (!kb.Governance.Enabled || versionID == "" || versionID == strings.TrimSpace(knowledge.CurrentVersionID)) {
		if err := EnqueueWikiIngest(ctx, s.taskEnqueuer, s.redisClient, payload.TenantID, payload.KnowledgeBaseID, payload.KnowledgeID, versionID); err != nil {
			return fmt.Errorf("enqueue wiki ingest: %w", err)
		}
		logger.Infof(ctx, "[KnowledgePostProcess] Enqueued wiki ingest task for %s", payload.KnowledgeID)
	}

	if kb.Governance.Enabled && versionID == "" {
		versionID = strings.TrimSpace(knowledge.PendingVersionID)
	}
	if kb.Governance.Enabled && versionID != "" && versionID == strings.TrimSpace(knowledge.PendingVersionID) {
		publishPayload := types.KnowledgePublishPayload{
			TenantID: payload.TenantID, KnowledgeID: knowledge.ID, VersionID: versionID,
		}
		langfuse.InjectTracing(ctx, &publishPayload)
		payloadBytes, marshalErr := json.Marshal(publishPayload)
		if marshalErr != nil && enqueueErr == nil {
			enqueueErr = fmt.Errorf("marshal governed publication payload: %w", marshalErr)
		} else if marshalErr == nil {
			publishTask := asynq.NewTask(types.TypeKnowledgePublish, payloadBytes, asynq.Queue("low"), asynq.ProcessIn(8*time.Second), asynq.MaxRetry(12))
			if _, publishErr := s.taskEnqueuer.Enqueue(publishTask); publishErr != nil {
				logger.Errorf(ctx, "[KnowledgePostProcess] Failed to enqueue governed publication for %s: %v", knowledge.ID, publishErr)
				if enqueueErr == nil {
					enqueueErr = fmt.Errorf("enqueue governed publication: %w", publishErr)
				}
			}
		}
	}
	if enqueueErr != nil {
		return enqueueErr
	}
	return nil
}

func shouldGenerateDocumentSummary(textChunks []*types.Chunk) bool {
	return len(textChunks) > 0
}

func (s *KnowledgePostProcessService) enqueueSummaryGenerationTask(ctx context.Context, payload types.KnowledgePostProcessPayload) error {
	if s.taskEnqueuer == nil {
		return fmt.Errorf("task enqueuer is not configured")
	}

	taskPayload := types.SummaryGenerationPayload{
		TenantID:        payload.TenantID,
		KnowledgeBaseID: payload.KnowledgeBaseID,
		KnowledgeID:     payload.KnowledgeID,
		VersionID:       payload.VersionID,
		Language:        payload.Language,
	}
	langfuse.InjectTracing(ctx, &taskPayload)
	payloadBytes, err := json.Marshal(taskPayload)
	if err != nil {
		logger.Warnf(ctx, "[KnowledgePostProcess] Failed to marshal summary generation payload: %v", err)
		return err
	}

	task := asynq.NewTask(types.TypeSummaryGeneration, payloadBytes, asynq.Queue("low"), asynq.MaxRetry(3))
	if _, err := s.taskEnqueuer.Enqueue(task); err != nil {
		logger.Warnf(ctx, "[KnowledgePostProcess] Failed to enqueue summary generation for %s: %v", payload.KnowledgeID, err)
		return fmt.Errorf("enqueue summary generation: %w", err)
	} else {
		logger.Infof(ctx, "[KnowledgePostProcess] Enqueued summary generation task for %s", payload.KnowledgeID)
	}
	return nil
}

func (s *KnowledgePostProcessService) enqueueQuestionGenerationIfEnabled(ctx context.Context, payload types.KnowledgePostProcessPayload, kb *types.KnowledgeBase) error {
	if s.taskEnqueuer == nil {
		return fmt.Errorf("task enqueuer is not configured")
	}

	if kb.QuestionGenerationConfig == nil || !kb.QuestionGenerationConfig.Enabled {
		return nil
	}

	questionCount := kb.QuestionGenerationConfig.QuestionCount
	if questionCount <= 0 {
		questionCount = 3
	}
	if questionCount > 10 {
		questionCount = 10
	}

	taskPayload := types.QuestionGenerationPayload{
		TenantID:        payload.TenantID,
		KnowledgeBaseID: payload.KnowledgeBaseID,
		KnowledgeID:     payload.KnowledgeID,
		VersionID:       payload.VersionID,
		QuestionCount:   questionCount,
		Language:        payload.Language,
	}
	langfuse.InjectTracing(ctx, &taskPayload)
	payloadBytes, err := json.Marshal(taskPayload)
	if err != nil {
		logger.Warnf(ctx, "[KnowledgePostProcess] Failed to marshal question generation payload: %v", err)
		return err
	}

	task := asynq.NewTask(types.TypeQuestionGeneration, payloadBytes, asynq.Queue("low"), asynq.MaxRetry(3))
	if _, err := s.taskEnqueuer.Enqueue(task); err != nil {
		logger.Warnf(ctx, "[KnowledgePostProcess] Failed to enqueue question generation for %s: %v", payload.KnowledgeID, err)
		return fmt.Errorf("enqueue question generation: %w", err)
	} else {
		logger.Infof(ctx, "[KnowledgePostProcess] Enqueued question generation task for %s (count=%d)", payload.KnowledgeID, questionCount)
	}
	return nil
}
