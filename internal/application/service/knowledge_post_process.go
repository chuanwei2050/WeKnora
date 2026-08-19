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
	if kb.ExtractConfig.IngestionMode.Normalize() == types.GraphIngestionSignal {
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

	kb, err := s.kbService.GetKnowledgeBaseByIDOnly(ctx, payload.KnowledgeBaseID)
	if err != nil || kb == nil {
		return fmt.Errorf("get knowledge base %s: %w", payload.KnowledgeBaseID, err)
	}

	// 2. Fetch all chunks
	chunks, err := s.chunkService.ListChunksByKnowledgeID(ctx, payload.KnowledgeID)
	if err != nil {
		return fmt.Errorf("list chunks for knowledge %s: %w", payload.KnowledgeID, err)
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
		knowledge.UpdatedAt = time.Now()

		// Setup summary status. Keyword-only knowledge bases may not have a
		// summary model configured; those documents are ready after parsing.
		if len(textChunks) > 0 && kb.SummaryModelID != "" {
			knowledge.SummaryStatus = types.SummaryStatusPending
		} else {
			knowledge.SummaryStatus = types.SummaryStatusNone
		}

		if err := s.knowledgeRepo.UpdateKnowledge(ctx, knowledge); err != nil {
			logger.Warnf(ctx, "[KnowledgePostProcess] Failed to update knowledge status to completed: %v", err)
		} else {
			logger.Infof(ctx, "[KnowledgePostProcess] Knowledge %s marked as completed.", payload.KnowledgeID)
		}
	}

	// 4. Spawn Summary and Question Tasks
	if len(textChunks) > 0 && kb.SummaryModelID != "" {
		s.enqueueSummaryGenerationTask(ctx, payload)
	}
	if len(textChunks) > 0 {
		// Question generation only makes sense for RAG indexing (improves chunk recall).
		// Skip when only Wiki/Graph is enabled without vector/keyword search.
		if kb.NeedsEmbeddingModel() {
			s.enqueueQuestionGenerationIfEnabled(ctx, payload, kb)
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
			}
		}
		logger.Infof(ctx, "[KnowledgePostProcess] Spawning Graph RAG extract tasks for %d/%d eligible text-like chunks", graphCandidates, len(textChunks))
	}

	// 6. Spawn Wiki Ingest Task if wiki indexing is enabled in IndexingStrategy
	if kb.IndexingStrategy.WikiEnabled && len(textChunks) > 0 {
		EnqueueWikiIngest(ctx, s.taskEnqueuer, s.redisClient, payload.TenantID, payload.KnowledgeBaseID, payload.KnowledgeID)
		logger.Infof(ctx, "[KnowledgePostProcess] Enqueued wiki ingest task for %s", payload.KnowledgeID)
	}

	if kb.Governance.Enabled && knowledge.PendingVersionID != "" {
		publishPayload := types.KnowledgePublishPayload{
			TenantID: payload.TenantID, KnowledgeID: knowledge.ID, VersionID: knowledge.PendingVersionID,
		}
		langfuse.InjectTracing(ctx, &publishPayload)
		if payloadBytes, marshalErr := json.Marshal(publishPayload); marshalErr == nil {
			publishTask := asynq.NewTask(types.TypeKnowledgePublish, payloadBytes, asynq.Queue("low"), asynq.ProcessIn(8*time.Second), asynq.MaxRetry(12))
			if _, enqueueErr := s.taskEnqueuer.Enqueue(publishTask); enqueueErr != nil {
				logger.Errorf(ctx, "[KnowledgePostProcess] Failed to enqueue governed publication for %s: %v", knowledge.ID, enqueueErr)
			}
		}
	}
	return nil
}

func (s *KnowledgePostProcessService) enqueueSummaryGenerationTask(ctx context.Context, payload types.KnowledgePostProcessPayload) {
	if s.taskEnqueuer == nil {
		return
	}

	taskPayload := types.SummaryGenerationPayload{
		TenantID:        payload.TenantID,
		KnowledgeBaseID: payload.KnowledgeBaseID,
		KnowledgeID:     payload.KnowledgeID,
		Language:        payload.Language,
	}
	langfuse.InjectTracing(ctx, &taskPayload)
	payloadBytes, err := json.Marshal(taskPayload)
	if err != nil {
		logger.Warnf(ctx, "[KnowledgePostProcess] Failed to marshal summary generation payload: %v", err)
		return
	}

	task := asynq.NewTask(types.TypeSummaryGeneration, payloadBytes, asynq.Queue("low"), asynq.MaxRetry(3))
	if _, err := s.taskEnqueuer.Enqueue(task); err != nil {
		logger.Warnf(ctx, "[KnowledgePostProcess] Failed to enqueue summary generation for %s: %v", payload.KnowledgeID, err)
	} else {
		logger.Infof(ctx, "[KnowledgePostProcess] Enqueued summary generation task for %s", payload.KnowledgeID)
	}
}

func (s *KnowledgePostProcessService) enqueueQuestionGenerationIfEnabled(ctx context.Context, payload types.KnowledgePostProcessPayload, kb *types.KnowledgeBase) {
	if s.taskEnqueuer == nil {
		return
	}

	if kb.QuestionGenerationConfig == nil || !kb.QuestionGenerationConfig.Enabled {
		return
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
		QuestionCount:   questionCount,
		Language:        payload.Language,
	}
	langfuse.InjectTracing(ctx, &taskPayload)
	payloadBytes, err := json.Marshal(taskPayload)
	if err != nil {
		logger.Warnf(ctx, "[KnowledgePostProcess] Failed to marshal question generation payload: %v", err)
		return
	}

	task := asynq.NewTask(types.TypeQuestionGeneration, payloadBytes, asynq.Queue("low"), asynq.MaxRetry(3))
	if _, err := s.taskEnqueuer.Enqueue(task); err != nil {
		logger.Warnf(ctx, "[KnowledgePostProcess] Failed to enqueue question generation for %s: %v", payload.KnowledgeID, err)
	} else {
		logger.Infof(ctx, "[KnowledgePostProcess] Enqueued question generation task for %s (count=%d)", payload.KnowledgeID, questionCount)
	}
}
