package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	filesvc "github.com/Tencent/WeKnora/internal/application/service/file"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/utils/ollama"
	"github.com/Tencent/WeKnora/internal/models/vlm"
	"github.com/Tencent/WeKnora/internal/tracing/langfuse"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

const (
	vlmOCRPrompt = "<system_prompt>\n" +
		"You are an OCR assistant. Your task is to extract all body text content from this document image and output in pure Markdown format.\n" +
		"</system_prompt>\n\n" +
		"<instructions>\n" +
		"1. Ignore headers and footers.\n" +
		"2. Use Markdown table syntax for tables.\n" +
		"3. Use LaTeX format for formulas (wrapped with $ or $$).\n" +
		"4. Organize content in the original reading order.\n" +
		"5. Output ONLY the extracted text content. Do NOT include any HTML tags, reasoning, or unrelated comments.\n" +
		"6. If there is absolutely no recognizable text content in the image, reply ONLY with: No text content.\n" +
		"</instructions>"
	vlmOCRScannedPDFPrompt = "<system_prompt>\n" +
		"You are an OCR and document layout extraction assistant. The input image is a page from a scanned PDF document.\n" +
		"Your task is to carefully extract all text and layout structure from the image, and output the result in pure Markdown format.\n" +
		"</system_prompt>\n\n" +
		"<instructions>\n" +
		"1. Ignore headers, footers, and page numbers.\n" +
		"2. Preserve the original document's paragraph and hierarchical structure as much as possible.\n" +
		"3. If there are tables, use Markdown table syntax to represent them.\n" +
		"4. If there are mathematical formulas, use LaTeX format wrapped in $ or $$.\n" +
		"5. Output ONLY the extracted text content. Do NOT include any HTML tags, reasoning, or unrelated comments.\n" +
		"6. If there is absolutely no recognizable text content in the image, reply ONLY with: No text content.\n" +
		"</instructions>"
	vlmCaptionPrompt = "Provide a brief and concise description of the main content of the image in Chinese"
)

// ImageMultimodalService handles image:multimodal asynq tasks.
// It reads images from storage (via FileService for provider:// URLs),
// performs OCR and VLM caption, and creates child chunks.
type ImageMultimodalService struct {
	chunkService   interfaces.ChunkService
	modelService   interfaces.ModelService
	kbService      interfaces.KnowledgeBaseService
	knowledgeRepo  interfaces.KnowledgeRepository
	tenantRepo     interfaces.TenantRepository
	tenantService  interfaces.TenantService
	retrieveEngine interfaces.RetrieveEngineRegistry
	ollamaService  *ollama.OllamaService
	taskEnqueuer   interfaces.TaskEnqueuer
	redisClient    *redis.Client
}

func NewImageMultimodalService(
	chunkService interfaces.ChunkService,
	modelService interfaces.ModelService,
	kbService interfaces.KnowledgeBaseService,
	knowledgeRepo interfaces.KnowledgeRepository,
	tenantRepo interfaces.TenantRepository,
	tenantService interfaces.TenantService,
	retrieveEngine interfaces.RetrieveEngineRegistry,
	ollamaService *ollama.OllamaService,
	taskEnqueuer interfaces.TaskEnqueuer,
	redisClient *redis.Client,
) interfaces.TaskHandler {
	return &ImageMultimodalService{
		chunkService:   chunkService,
		modelService:   modelService,
		kbService:      kbService,
		knowledgeRepo:  knowledgeRepo,
		tenantRepo:     tenantRepo,
		tenantService:  tenantService,
		retrieveEngine: retrieveEngine,
		ollamaService:  ollamaService,
		taskEnqueuer:   taskEnqueuer,
		redisClient:    redisClient,
	}
}

func imageMultimodalVersionMatches(kb *types.KnowledgeBase, knowledge *types.Knowledge, versionID string) bool {
	if kb == nil || !kb.Governance.Enabled {
		return true
	}
	versionID = strings.TrimSpace(versionID)
	if versionID == "" || knowledge == nil {
		return false
	}
	return strings.TrimSpace(knowledge.PendingVersionID) == versionID ||
		strings.TrimSpace(knowledge.CurrentVersionID) == versionID
}

func (s *ImageMultimodalService) cleanupImageChunks(
	ctx context.Context,
	payload types.ImageMultimodalPayload,
	kb *types.KnowledgeBase,
	chunks []*types.Chunk,
) {
	chunkIDs := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk != nil && chunk.ID != "" {
			chunkIDs = append(chunkIDs, chunk.ID)
		}
	}
	if len(chunkIDs) == 0 {
		return
	}
	if err := s.chunkService.DeleteChunks(ctx, chunkIDs); err != nil {
		logger.Warnf(ctx, "[ImageMultimodal] Failed to cleanup chunks: %v", err)
	}
	if kb == nil || !kb.NeedsEmbeddingModel() {
		return
	}
	embeddingModel, err := s.modelService.GetEmbeddingModel(ctx, kb.EmbeddingModelID)
	if err != nil {
		logger.Warnf(ctx, "[ImageMultimodal] Failed to resolve embedding model for cleanup: %v", err)
		return
	}
	tenantInfo, err := s.tenantService.GetTenantByID(ctx, payload.TenantID)
	if err != nil || tenantInfo == nil {
		logger.Warnf(ctx, "[ImageMultimodal] Failed to resolve tenant for cleanup: %v", err)
		return
	}
	engine, err := retriever.NewCompositeRetrieveEngine(s.retrieveEngine, tenantInfo.GetEffectiveEngines())
	if err != nil {
		logger.Warnf(ctx, "[ImageMultimodal] Failed to init retrieve engine for cleanup: %v", err)
		return
	}
	if err := engine.DeleteByChunkIDList(ctx, chunkIDs, embeddingModel.GetDimensions(), kb.Type); err != nil {
		logger.Warnf(ctx, "[ImageMultimodal] Failed to cleanup indexes: %v", err)
	}
}

func (s *ImageMultimodalService) cleanupStaleImageSource(ctx context.Context, payload types.ImageMultimodalPayload) {
	if types.ParseProviderScheme(payload.ImageURL) == "" {
		return
	}
	fileSvc := s.resolveFileServiceForPayload(ctx, payload)
	if fileSvc == nil {
		return
	}
	if err := fileSvc.DeleteFile(ctx, payload.ImageURL); err != nil {
		logger.Warnf(ctx, "[ImageMultimodal] Failed to cleanup stale image source %s: %v", payload.ImageURL, err)
	}
}

// Handle implements asynq handler for TypeImageMultimodal.
func (s *ImageMultimodalService) Handle(ctx context.Context, task *asynq.Task) error {
	retryCount, retryCountOK := asynq.GetRetryCount(ctx)
	maxRetry, maxRetryOK := asynq.GetMaxRetry(ctx)
	isLastRetry := retryCountOK && maxRetryOK && retryCount >= maxRetry

	var payload types.ImageMultimodalPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal image multimodal payload: %w", err)
	}

	logger.Infof(ctx, "[ImageMultimodal] Processing image: chunk=%s, url=%s, ocr=%v, caption=%v",
		payload.ChunkID, payload.ImageURL, payload.EnableOCR, payload.EnableCaption)

	ctx = context.WithValue(ctx, types.TenantIDContextKey, payload.TenantID)
	if payload.Language != "" {
		ctx = context.WithValue(ctx, types.LanguageContextKey, payload.Language)
	}
	knowledge, err := s.knowledgeRepo.GetKnowledgeByID(ctx, payload.TenantID, payload.KnowledgeID)
	if err != nil {
		return fmt.Errorf("get knowledge for image multimodal task: %w", err)
	}
	if knowledge == nil {
		return nil
	}
	if knowledge.KnowledgeBaseID != payload.KnowledgeBaseID {
		return fmt.Errorf("image multimodal task knowledge base mismatch")
	}
	kb, err := s.kbService.GetKnowledgeBaseByIDOnly(ctx, payload.KnowledgeBaseID)
	if err != nil || kb == nil {
		if err == nil {
			err = fmt.Errorf("knowledge base not found")
		}
		return fmt.Errorf("get knowledge base for image multimodal task: %w", err)
	}
	if !imageMultimodalVersionMatches(kb, knowledge, payload.VersionID) {
		logger.Infof(ctx, "[ImageMultimodal] Skipping stale governed version %s for knowledge %s", payload.VersionID, payload.KnowledgeID)
		s.cleanupStaleImageSource(ctx, payload)
		return nil
	}

	vlmModel, err := s.resolveVLM(ctx, payload.KnowledgeBaseID)
	if err != nil {
		return fmt.Errorf("resolve VLM: %w", err)
	}

	// Read image bytes: try provider:// via tenant-resolved FileService,
	// then legacy local path, then HTTP URL.
	var imgBytes []byte
	if types.ParseProviderScheme(payload.ImageURL) != "" {
		fileSvc := s.resolveFileServiceForPayload(ctx, payload)
		if fileSvc == nil {
			logger.Warnf(ctx, "[ImageMultimodal] Resolve tenant file service failed, fallback to URL/local: tenant=%d kb=%s",
				payload.TenantID, payload.KnowledgeBaseID)
		} else {
			// provider:// scheme — read via FileService
			reader, getErr := fileSvc.GetFile(ctx, payload.ImageURL)
			if getErr != nil {
				logger.Warnf(ctx, "[ImageMultimodal] FileService.GetFile(%s) failed: %v", payload.ImageURL, getErr)
			} else {
				imgBytes, err = io.ReadAll(reader)
				reader.Close()
				if err != nil {
					logger.Warnf(ctx, "[ImageMultimodal] Read provider file %s failed: %v", payload.ImageURL, err)
					imgBytes = nil
				}
			}
		}
	}
	if imgBytes == nil && payload.ImageLocalPath != "" {
		imgBytes, err = os.ReadFile(payload.ImageLocalPath)
		if err != nil {
			logger.Warnf(ctx, "[ImageMultimodal] Local file %s not available (%v), trying URL", payload.ImageLocalPath, err)
			imgBytes = nil
		}
	}
	if imgBytes == nil {
		imgBytes, err = downloadImageFromURL(payload.ImageURL)
		if err != nil {
			logger.Errorf(ctx, "[ImageMultimodal] Failed to download image from URL %s: %v", payload.ImageURL, err)
			return fmt.Errorf("read image from URL %s failed: %w", payload.ImageURL, err)
		}
		logger.Infof(ctx, "[ImageMultimodal] Image downloaded from URL, len=%d", len(imgBytes))
	}

	imageInfo := types.ImageInfo{
		URL:         payload.ImageURL,
		OriginalURL: payload.ImageURL,
	}

	if payload.EnableOCR {
		prompt := vlmOCRPrompt
		if payload.ImageSourceType == "scanned_pdf" {
			prompt = vlmOCRScannedPDFPrompt
			logger.Infof(ctx, "[ImageMultimodal] Using scanned PDF prompt for OCR: %s", payload.ImageURL)
		}

		ocrText, ocrErr := vlmModel.Predict(ctx, [][]byte{imgBytes}, prompt)
		if ocrErr != nil {
			return s.handleVLMFailure(ctx, payload, "OCR", ocrErr, isLastRetry)
		} else {
			ocrText = sanitizeOCRText(ocrText)
			if ocrText != "" {
				imageInfo.OCRText = ocrText
			} else {
				logger.Warnf(ctx, "[ImageMultimodal] OCR returned empty/invalid content for %s, discarded", payload.ImageURL)
			}
		}
	}

	if payload.EnableCaption {
		caption, capErr := vlmModel.Predict(ctx, [][]byte{imgBytes}, vlmCaptionPrompt)
		if capErr != nil {
			return s.handleVLMFailure(ctx, payload, "caption", capErr, isLastRetry)
		} else if caption != "" {
			imageInfo.Caption = caption
		}
	}

	latestKnowledge, err := s.knowledgeRepo.GetKnowledgeByID(ctx, payload.TenantID, payload.KnowledgeID)
	if err != nil {
		return fmt.Errorf("reload knowledge after image multimodal processing: %w", err)
	}
	if latestKnowledge == nil || latestKnowledge.KnowledgeBaseID != payload.KnowledgeBaseID || !imageMultimodalVersionMatches(kb, latestKnowledge, payload.VersionID) {
		logger.Infof(ctx, "[ImageMultimodal] Version %s was superseded while processing %s, skipping results", payload.VersionID, payload.KnowledgeID)
		s.cleanupStaleImageSource(ctx, payload)
		return nil
	}
	knowledge = latestKnowledge

	// Build child chunks for OCR and caption results
	imageInfoJSON, _ := json.Marshal([]types.ImageInfo{imageInfo})
	var newChunks []*types.Chunk

	if imageInfo.OCRText != "" {
		newChunks = append(newChunks, &types.Chunk{
			ID:                 uuid.New().String(),
			TenantID:           payload.TenantID,
			KnowledgeID:        payload.KnowledgeID,
			KnowledgeBaseID:    payload.KnowledgeBaseID,
			KnowledgeVersionID: payload.VersionID,
			TagID:              knowledge.TagID,
			Content:            imageInfo.OCRText,
			ChunkType:          types.ChunkTypeImageOCR,
			ParentChunkID:      payload.ChunkID,
			IsEnabled:          true,
			Flags:              types.ChunkFlagRecommended,
			ImageInfo:          string(imageInfoJSON),
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
		})
	}

	if imageInfo.Caption != "" {
		newChunks = append(newChunks, &types.Chunk{
			ID:                 uuid.New().String(),
			TenantID:           payload.TenantID,
			KnowledgeID:        payload.KnowledgeID,
			KnowledgeBaseID:    payload.KnowledgeBaseID,
			KnowledgeVersionID: payload.VersionID,
			TagID:              knowledge.TagID,
			Content:            imageInfo.Caption,
			ChunkType:          types.ChunkTypeImageCaption,
			ParentChunkID:      payload.ChunkID,
			IsEnabled:          true,
			Flags:              types.ChunkFlagRecommended,
			ImageInfo:          string(imageInfoJSON),
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
		})
	}

	if len(newChunks) == 0 {
		if latest, checkErr := s.knowledgeRepo.GetKnowledgeByID(ctx, payload.TenantID, payload.KnowledgeID); checkErr != nil {
			return fmt.Errorf("reload knowledge before image multimodal finalization: %w", checkErr)
		} else if latest == nil || !imageMultimodalVersionMatches(kb, latest, payload.VersionID) {
			s.cleanupStaleImageSource(ctx, payload)
			return nil
		}
		return s.checkAndFinalizeAllImages(ctx, payload)
	}

	// Persist chunks
	if err := s.chunkService.CreateChunks(ctx, newChunks); err != nil {
		s.cleanupImageChunks(ctx, payload, kb, newChunks)
		return fmt.Errorf("create multimodal chunks: %w", err)
	}
	for _, c := range newChunks {
		logger.Infof(ctx, "[ImageMultimodal] Created %s chunk %s for image %s, len=%d",
			c.ChunkType, c.ID, payload.ImageURL, len(c.Content))
	}

	if latest, checkErr := s.knowledgeRepo.GetKnowledgeByID(ctx, payload.TenantID, payload.KnowledgeID); checkErr != nil {
		s.cleanupImageChunks(ctx, payload, kb, newChunks)
		return fmt.Errorf("reload knowledge before image multimodal indexing: %w", checkErr)
	} else if latest == nil || !imageMultimodalVersionMatches(kb, latest, payload.VersionID) {
		logger.Infof(ctx, "[ImageMultimodal] Version %s was superseded before indexing %s, cleaning results", payload.VersionID, payload.KnowledgeID)
		s.cleanupImageChunks(ctx, payload, kb, newChunks)
		s.cleanupStaleImageSource(ctx, payload)
		return nil
	}

	// Index chunks so they can be retrieved
	if err := s.indexChunks(ctx, payload, newChunks); err != nil {
		s.cleanupImageChunks(ctx, payload, kb, newChunks)
		return fmt.Errorf("index multimodal chunks: %w", err)
	}
	if latest, checkErr := s.knowledgeRepo.GetKnowledgeByID(ctx, payload.TenantID, payload.KnowledgeID); checkErr != nil {
		s.cleanupImageChunks(ctx, payload, kb, newChunks)
		return fmt.Errorf("reload knowledge after image multimodal indexing: %w", checkErr)
	} else if latest == nil || !imageMultimodalVersionMatches(kb, latest, payload.VersionID) {
		logger.Infof(ctx, "[ImageMultimodal] Version %s was superseded after indexing %s, cleaning results", payload.VersionID, payload.KnowledgeID)
		s.cleanupImageChunks(ctx, payload, kb, newChunks)
		s.cleanupStaleImageSource(ctx, payload)
		return nil
	}

	// Enqueue question generation for the caption/OCR content if KB has it enabled.
	// During initial processChunks, question generation is skipped for image-type
	// knowledge because the text chunk is just a markdown reference. Now that we
	// have real textual content (caption/OCR), we can generate questions.
	// Note: for documents with multiple images (e.g. PDFs), we also wait until
	// all images are processed before triggering summary/question generation.
	return s.checkAndFinalizeAllImages(ctx, payload)
}

func (s *ImageMultimodalService) handleVLMFailure(
	ctx context.Context,
	payload types.ImageMultimodalPayload,
	operation string,
	cause error,
	isLastRetry bool,
) error {
	if !isLastRetry {
		logger.Warnf(ctx, "[ImageMultimodal] %s failed for %s, retrying: %v", operation, payload.ImageURL, cause)
		return fmt.Errorf("%s failed for %s: %w", operation, payload.ImageURL, cause)
	}

	logger.Errorf(ctx, "[ImageMultimodal] %s permanently failed for %s; finalizing document without this image enrichment: %v", operation, payload.ImageURL, cause)
	return s.checkAndFinalizeAllImages(ctx, payload)
}

// indexChunks indexes the newly created multimodal chunks into the retrieval engine
// so they can participate in semantic search.
func (s *ImageMultimodalService) indexChunks(ctx context.Context, payload types.ImageMultimodalPayload, chunks []*types.Chunk) error {
	kb, err := s.kbService.GetKnowledgeBaseByIDOnly(ctx, payload.KnowledgeBaseID)
	if err != nil || kb == nil {
		logger.Warnf(ctx, "[ImageMultimodal] Failed to get KB for indexing: %v", err)
		if err == nil {
			err = fmt.Errorf("knowledge base not found")
		}
		return err
	}

	// Skip vector/keyword indexing when the KB has no embedding-based pipeline enabled
	// (e.g. Wiki-only KBs). Without this check, GetEmbeddingModel would fail because
	// EmbeddingModelID is intentionally empty for such KBs. The multimodal chunks
	// themselves are already persisted in the DB above, so skipping index here is safe.
	if !kb.NeedsEmbeddingModel() {
		logger.Infof(ctx, "[ImageMultimodal] Vector/keyword indexing disabled for KB %s, skipping index for %d multimodal chunks",
			kb.ID, len(chunks))
		// Still mark chunks as indexed so downstream finalization sees a consistent state.
		for _, chunk := range chunks {
			dbChunk, gerr := s.chunkService.GetChunkByIDOnly(ctx, chunk.ID)
			if gerr != nil {
				return fmt.Errorf("fetch chunk %s for status update: %w", chunk.ID, gerr)
			}
			dbChunk.Status = int(types.ChunkStatusIndexed)
			if uerr := s.chunkService.UpdateChunk(ctx, dbChunk); uerr != nil {
				return fmt.Errorf("update chunk %s status: %w", chunk.ID, uerr)
			}
		}
		return nil
	}

	embeddingModel, err := s.modelService.GetEmbeddingModel(ctx, kb.EmbeddingModelID)
	if err != nil {
		logger.Warnf(ctx, "[ImageMultimodal] Failed to get embedding model for indexing: %v", err)
		return fmt.Errorf("get embedding model: %w", err)
	}

	tenantInfo, err := s.tenantService.GetTenantByID(ctx, payload.TenantID)
	if err != nil {
		logger.Warnf(ctx, "[ImageMultimodal] Failed to get tenant for indexing: %v", err)
		return fmt.Errorf("get tenant: %w", err)
	}
	if tenantInfo == nil {
		return fmt.Errorf("tenant not found")
	}

	engine, err := retriever.NewCompositeRetrieveEngine(s.retrieveEngine, tenantInfo.GetEffectiveEngines())
	if err != nil {
		logger.Warnf(ctx, "[ImageMultimodal] Failed to init retrieve engine: %v", err)
		return fmt.Errorf("init retrieve engine: %w", err)
	}

	indexInfoList := make([]*types.IndexInfo, 0, len(chunks))
	for _, chunk := range chunks {
		indexInfoList = append(indexInfoList, documentChunkIndexInfo(chunk, chunk.Content, chunk.ID))
	}

	if err := engine.BatchIndex(ctx, embeddingModel, indexInfoList); err != nil {
		logger.Errorf(ctx, "[ImageMultimodal] Failed to index multimodal chunks: %v", err)
		return err
	}

	// Mark chunks as indexed.
	// Must re-fetch from DB because the in-memory objects lack auto-generated fields
	// (e.g. seq_id), and GORM Save would overwrite them with zero values.
	for _, chunk := range chunks {
		dbChunk, err := s.chunkService.GetChunkByIDOnly(ctx, chunk.ID)
		if err != nil {
			return fmt.Errorf("fetch chunk %s for status update: %w", chunk.ID, err)
		}
		dbChunk.Status = int(types.ChunkStatusIndexed)
		if err := s.chunkService.UpdateChunk(ctx, dbChunk); err != nil {
			return fmt.Errorf("update chunk %s status: %w", chunk.ID, err)
		}
	}

	logger.Infof(ctx, "[ImageMultimodal] Indexed %d multimodal chunks for image %s", len(chunks), payload.ImageURL)
	return nil
}

// resolveVLM creates a vlm.VLM instance for the given knowledge base,
// supporting both new-style (ModelID) and legacy (inline BaseURL) configs.
func (s *ImageMultimodalService) resolveVLM(ctx context.Context, kbID string) (vlm.VLM, error) {
	kb, err := s.kbService.GetKnowledgeBaseByIDOnly(ctx, kbID)
	if err != nil {
		return nil, fmt.Errorf("get knowledge base %s: %w", kbID, err)
	}
	if kb == nil {
		return nil, fmt.Errorf("knowledge base %s not found", kbID)
	}

	vlmCfg := kb.VLMConfig
	if !vlmCfg.IsEnabled() {
		return nil, fmt.Errorf("VLM is not enabled for knowledge base %s", kbID)
	}

	// Platform-managed configs only need the feature toggle. Inline parameters
	// are retained solely for legacy knowledge bases.
	if vlmCfg.ModelID != "" || (vlmCfg.ModelName == "" && vlmCfg.BaseURL == "") {
		return s.modelService.GetVLMModel(ctx, "")
	}

	// Legacy: create VLM from inline config
	return vlm.NewVLMFromLegacyConfig(vlmCfg, s.ollamaService)
}

// resolveFileServiceForPayload resolves tenant/KB scoped file service for reading provider:// URLs.
func (s *ImageMultimodalService) resolveFileServiceForPayload(ctx context.Context, payload types.ImageMultimodalPayload) interfaces.FileService {
	tenant, err := s.tenantService.GetTenantByID(ctx, payload.TenantID)
	if err != nil || tenant == nil {
		logger.Warnf(ctx, "[ImageMultimodal] GetTenantByID failed: tenant=%d err=%v", payload.TenantID, err)
		return nil
	}

	provider := types.ParseProviderScheme(payload.ImageURL)
	if provider == "" && tenant.StorageEngineConfig != nil {
		provider = strings.ToLower(strings.TrimSpace(tenant.StorageEngineConfig.DefaultProvider))
	}

	baseDir := strings.TrimSpace(os.Getenv("LOCAL_STORAGE_BASE_DIR"))
	fileSvc, _, svcErr := filesvc.NewFileServiceFromStorageConfig(provider, tenant.StorageEngineConfig, baseDir)
	if svcErr != nil {
		logger.Warnf(ctx, "[ImageMultimodal] resolve file service failed: tenant=%d provider=%s err=%v", payload.TenantID, provider, svcErr)
		return nil
	}
	return fileSvc
}

// downloadImageFromURL downloads image bytes from an HTTP(S) URL.
func downloadImageFromURL(imageURL string) ([]byte, error) {
	return secutils.DownloadBytes(imageURL)
}

func (s *ImageMultimodalService) checkAndFinalizeAllImages(ctx context.Context, payload types.ImageMultimodalPayload) error {
	if s.redisClient == nil {
		return s.enqueueKnowledgePostProcessTask(ctx, payload)
	}

	redisKey := multimodalPendingKey(payload.KnowledgeID, payload.VersionID)

	pendingCount, err := s.redisClient.Decr(ctx, redisKey).Result()
	if err != nil && err != redis.Nil {
		logger.Warnf(ctx, "[ImageMultimodal] Failed to decrement pending count for %s: %v", payload.KnowledgeID, err)
		return fmt.Errorf("decrement multimodal pending count: %w", err)
	}

	if pendingCount <= 0 {
		logger.Infof(ctx, "[ImageMultimodal] All images processed for knowledge %s. Finalizing...", payload.KnowledgeID)
		if err := s.redisClient.Del(ctx, redisKey).Err(); err != nil && err != redis.Nil {
			return fmt.Errorf("clear multimodal pending count: %w", err)
		}

		return s.enqueueKnowledgePostProcessTask(ctx, payload)
	}
	return nil
}

func (s *ImageMultimodalService) enqueueKnowledgePostProcessTask(ctx context.Context, payload types.ImageMultimodalPayload) error {
	if s.taskEnqueuer == nil {
		return fmt.Errorf("task enqueuer is not configured")
	}

	taskPayload := types.KnowledgePostProcessPayload{
		TenantID:        payload.TenantID,
		KnowledgeID:     payload.KnowledgeID,
		KnowledgeBaseID: payload.KnowledgeBaseID,
		VersionID:       payload.VersionID,
		Language:        payload.Language,
	}
	langfuse.InjectTracing(ctx, &taskPayload)
	payloadBytes, err := json.Marshal(taskPayload)
	if err != nil {
		logger.Warnf(ctx, "[ImageMultimodal] Failed to marshal post process payload: %v", err)
		return err
	}

	task := asynq.NewTask(types.TypeKnowledgePostProcess, payloadBytes, asynq.Queue("default"), asynq.MaxRetry(3))
	if _, err := s.taskEnqueuer.Enqueue(task); err != nil {
		logger.Warnf(ctx, "[ImageMultimodal] Failed to enqueue post process task for %s: %v", payload.KnowledgeID, err)
		return fmt.Errorf("enqueue knowledge post process: %w", err)
	} else {
		logger.Infof(ctx, "[ImageMultimodal] Enqueued post process task for %s", payload.KnowledgeID)
	}
	return nil
}
