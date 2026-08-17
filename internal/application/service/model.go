package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/asr"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/models/provider"
	"github.com/Tencent/WeKnora/internal/models/rerank"
	"github.com/Tencent/WeKnora/internal/models/transport"
	"github.com/Tencent/WeKnora/internal/models/tts"
	"github.com/Tencent/WeKnora/internal/models/utils/ollama"
	"github.com/Tencent/WeKnora/internal/models/vlm"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
)

// ErrModelNotFound is returned when a model cannot be found in the repository
var ErrModelNotFound = errors.New("model not found")

// modelService implements the model service interface
type modelService struct {
	repo              interfaces.ModelRepository
	ollamaService     *ollama.OllamaService
	pooler            embedding.EmbedderPooler
	tenantService     interfaces.TenantService
	approvedEndpoints interfaces.ApprovedEndpointRepository
}

// NewModelService creates a new model service instance
func NewModelService(repo interfaces.ModelRepository,
	ollamaService *ollama.OllamaService,
	pooler embedding.EmbedderPooler,
	tenantService interfaces.TenantService,
	approvedEndpoints interfaces.ApprovedEndpointRepository,
) interfaces.ModelService {
	return &modelService{
		repo:              repo,
		ollamaService:     ollamaService,
		pooler:            pooler,
		tenantService:     tenantService,
		approvedEndpoints: approvedEndpoints,
	}
}

// decryptAppSecret 解密 AppSecret（如果为空或 cryptoSvc 为空则原样返回）
func (s *modelService) decryptAppSecret(encrypted string) string {
	if encrypted == "" {
		return encrypted
	}
	if key := utils.GetAESKey(); key != nil {
		if encrypted, err := utils.DecryptAESGCM(encrypted, key); err == nil {
			return encrypted
		}
	}
	return encrypted
}

// resolveWeKnoraCloudCredentials 为 WeKnoraCloud 厂商模型补全 AppID/AppSecret。
// 当模型自身参数中未存储凭证时，自动从租户配置中获取（SaveCredentials 保存的凭证）。
func (s *modelService) resolveWeKnoraCloudCredentials(ctx context.Context, params *types.ModelParameters) (appID, appSecret string) {
	appID = params.AppID
	appSecret = s.decryptAppSecret(params.AppSecret)

	if provider.ProviderName(params.Provider) != provider.ProviderWeKnoraCloud {
		return
	}
	if appID != "" && appSecret != "" {
		return
	}

	if s.tenantService == nil {
		return
	}
	creds := s.tenantService.GetWeKnoraCloudCredentials(ctx)
	if creds == nil {
		return
	}
	if appID == "" {
		appID = creds.AppID
	}
	if appSecret == "" {
		appSecret = creds.AppSecret
	}
	return
}

// CreateModel creates a new model in the repository
// For local models, it initiates an asynchronous download process
// Remote models are immediately set to active status
func (s *modelService) CreateModel(ctx context.Context, model *types.Model) error {
	if model == nil {
		return errors.New("model is required")
	}
	logger.Infof(ctx, "Creating model: %s, type: %s, source: %s", model.Name, model.Type, model.Source)
	normalizeModelParameters(model)
	if err := s.validateApprovedModelEndpoint(ctx, model); err != nil {
		return err
	}
	if err := validateDeclaredModelCapabilities(model); err != nil {
		return err
	}
	if airGappedMode() && (model.Parameters.Location == types.EndpointPublic || model.Parameters.Location == types.EndpointUnknown) {
		return errors.New("air-gapped mode rejects public model endpoints")
	}

	// Handle remote models (e.g., OpenAI, Azure)
	if model.Source == types.ModelSourceRemote {
		logger.Info(ctx, "Remote model detected, setting status to active")
		model.Status = types.ModelStatusActive

		logger.Info(ctx, "Saving remote model to repository")
		err := s.repo.Create(ctx, model)
		if err != nil {
			logger.ErrorWithFields(ctx, err, map[string]interface{}{
				"model_name": model.Name,
				"model_type": model.Type,
			})
			return err
		}

		logger.Infof(ctx, "Remote model created successfully: %s", model.ID)
		return nil
	}

	// Handle local models (e.g., Ollama)
	if model.Parameters.ArtifactPolicy == types.ArtifactPreloadedOnly || strings.EqualFold(os.Getenv("AIR_GAPPED_MODE"), "true") {
		model.Status = types.ModelStatusActive
		if s.ollamaService == nil {
			return errors.New("preloaded Ollama model requires an Ollama service")
		}
		available, err := s.ollamaService.IsModelAvailable(ctx, model.Name)
		if err != nil {
			return fmt.Errorf("check preloaded model: %w", err)
		}
		if !available {
			model.Status = types.ModelStatusDownloadFailed
			if err := s.repo.Create(ctx, model); err != nil {
				return err
			}
			return errors.New("preloaded model is missing; runtime download is disabled")
		}
		return s.repo.Create(ctx, model)
	}
	logger.Info(ctx, "Local model detected, setting status to downloading")
	model.Status = types.ModelStatusDownloading

	logger.Info(ctx, "Saving local model to repository")
	err := s.repo.Create(ctx, model)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_name": model.Name,
			"model_type": model.Type,
		})
		return err
	}

	// Start asynchronous model download
	logger.Infof(ctx, "Starting background download for model: %s", model.Name)
	newCtx := logger.CloneContext(ctx)
	go func() {
		logger.Info(newCtx, "Background download started")
		err := s.ollamaService.PullModel(newCtx, model.Name)
		if err != nil {
			logger.ErrorWithFields(newCtx, err, map[string]interface{}{
				"model_name": model.Name,
			})
			model.Status = types.ModelStatusDownloadFailed
		} else {
			logger.Infof(newCtx, "Model download completed successfully: %s", model.Name)
			model.Status = types.ModelStatusActive
		}
		logger.Infof(newCtx, "Updating model status to: %s", model.Status)
		s.repo.Update(newCtx, model)
	}()

	logger.Infof(ctx, "Model creation initiated successfully: %s", model.ID)
	return nil
}

func airGappedMode() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("AIR_GAPPED_MODE")), "true")
}

func (s *modelService) validateApprovedModelEndpoint(ctx context.Context, model *types.Model) error {
	if model == nil || model.Parameters.BaseURL == "" || model.Source == types.ModelSourceLocal {
		return nil
	}
	if strings.TrimSpace(model.Parameters.ApprovedEndpointID) == "" {
		host := endpointHost(model.Parameters.BaseURL)
		ips, lookupErr := net.LookupIP(host)
		location := types.DeriveEndpointLocation(model.Parameters.BaseURL, ips)
		if lookupErr != nil || location != types.EndpointPublic {
			return errors.New("private or unresolved model endpoints require an approved endpoint")
		}
		if airGappedMode() {
			return errors.New("air-gapped remote models require an approved endpoint")
		}
		return nil
	}
	if s.approvedEndpoints == nil {
		return errors.New("approved endpoint repository is unavailable")
	}
	endpoint, err := s.approvedEndpoints.GetByID(ctx, model.TenantID, model.Parameters.ApprovedEndpointID)
	if err != nil {
		return fmt.Errorf("load approved model endpoint: %w", err)
	}
	if endpoint == nil {
		return errors.New("approved model endpoint not found")
	}
	role := modelRoleForType(model.Type)
	if len(endpoint.AllowedModelRoles) > 0 {
		allowed := false
		for _, candidate := range endpoint.AllowedModelRoles {
			if candidate == role {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("approved endpoint is not allowed for model role %q", role)
		}
	}
	host := endpointHost(model.Parameters.BaseURL)
	ips, lookupErr := net.LookupIP(host)
	if lookupErr != nil {
		return fmt.Errorf("resolve model endpoint: %w", lookupErr)
	}
	if err := endpoint.ValidateDeploymentAllowlist(utils.IsSSRFWhitelisted, ips, airGappedMode()); err != nil {
		return err
	}
	use := model.Parameters.EndpointUse
	if use == "" {
		use = "model"
	}
	return endpoint.ValidateConnection(model.Parameters.BaseURL, types.EndpointCategoryModel, use, ips, airGappedMode())
}

func (s *modelService) modelIPValidator(ctx context.Context, model *types.Model) (func(net.IP) error, error) {
	if model == nil || model.Parameters.BaseURL == "" || model.Parameters.ApprovedEndpointID == "" || s.approvedEndpoints == nil {
		return nil, nil
	}
	endpoint, err := s.approvedEndpoints.GetByID(ctx, model.TenantID, model.Parameters.ApprovedEndpointID)
	if err != nil || endpoint == nil {
		if err == nil {
			err = errors.New("approved model endpoint not found")
		}
		return nil, err
	}
	initialIPs, err := net.LookupIP(endpoint.Host)
	if err != nil {
		return nil, err
	}
	allowed := make([]net.IP, len(initialIPs))
	copy(allowed, initialIPs)
	return func(ip net.IP) error {
		for _, candidate := range allowed {
			if candidate.Equal(ip) {
				if airGappedMode() && !utils.IsSSRFWhitelisted(endpoint.Host) && !utils.IsSSRFWhitelisted(ip.String()) {
					return fmt.Errorf("model endpoint IP is not in the deployment SSRF allowlist")
				}
				return nil
			}
		}
		return fmt.Errorf("model endpoint DNS result changed during connection")
	}, nil
}

func modelRoleForType(modelType types.ModelType) types.ModelRole {
	switch modelType {
	case types.ModelTypeEmbedding:
		return types.ModelRoleEmbedding
	case types.ModelTypeRerank:
		return types.ModelRoleRerank
	case types.ModelTypeVLM:
		return types.ModelRoleVLM
	case types.ModelTypeASR:
		return types.ModelRoleASR
	case types.ModelTypeTTS:
		return types.ModelRoleTTS
	case types.ModelTypeVerifier:
		return types.ModelRoleVerifier
	case types.ModelTypeJudge:
		return types.ModelRoleEvaluationJudge
	case types.ModelTypeParserOCR:
		return types.ModelRoleParserOCR
	default:
		return types.ModelRoleChat
	}
}

// normalizeModelParameters derives deployment metadata at the service boundary.
// A client-provided location is never trusted for security decisions.
func normalizeModelParameters(model *types.Model) {
	params := &model.Parameters
	if params.Protocol == "" {
		if model.Source == types.ModelSourceLocal {
			params.Protocol = types.ModelProtocolOllama
		} else {
			params.Protocol = types.ModelProtocolOpenAICompatible
		}
	}
	if params.ArtifactPolicy == "" {
		if airGappedMode() {
			params.ArtifactPolicy = types.ArtifactPreloadedOnly
		} else {
			params.ArtifactPolicy = types.ArtifactAllowDownload
		}
	}
	if model.Source == types.ModelSourceLocal && params.BaseURL == "" {
		params.Location = types.EndpointSameHost
		return
	}
	if params.BaseURL == "" {
		params.Location = types.EndpointUnknown
		return
	}
	resolved, err := net.LookupIP(strings.TrimSpace(endpointHost(params.BaseURL)))
	params.Location = types.DeriveEndpointLocation(params.BaseURL, func() []net.IP {
		if err != nil {
			return nil
		}
		return resolved
	}())
}

func endpointHost(raw string) string {
	scheme, host, _, err := types.NormalizeEndpoint(raw)
	_ = scheme
	if err != nil {
		return ""
	}
	return host
}

func validateDeclaredModelCapabilities(model *types.Model) error {
	if len(model.Parameters.Capabilities.Roles) == 0 {
		return nil
	}
	var role types.ModelRole
	switch model.Type {
	case types.ModelTypeKnowledgeQA, types.ModelTypeVLLM:
		role = types.ModelRoleChat
	case types.ModelTypeEmbedding:
		role = types.ModelRoleEmbedding
	case types.ModelTypeRerank:
		role = types.ModelRoleRerank
	case types.ModelTypeVLM:
		role = types.ModelRoleVLM
	case types.ModelTypeASR:
		role = types.ModelRoleASR
	case types.ModelTypeTTS:
		role = types.ModelRoleTTS
	case types.ModelTypeVerifier:
		role = types.ModelRoleVerifier
	case types.ModelTypeJudge:
		role = types.ModelRoleEvaluationJudge
	case types.ModelTypeParserOCR:
		role = types.ModelRoleParserOCR
	default:
		return fmt.Errorf("unsupported model type %q", model.Type)
	}
	return model.Parameters.Capabilities.ValidateRole(role)
}

// GetModelByID retrieves a model by its ID
// Returns an error if the model is not found or is in a non-active state
func (s *modelService) GetModelByID(ctx context.Context, id string) (*types.Model, error) {
	// Check if ID is empty
	if id == "" {
		logger.Error(ctx, "Model ID is empty")
		return nil, errors.New("model ID cannot be empty")
	}

	tenantID := types.MustTenantIDFromContext(ctx)

	// Fetch model from repository
	model, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id":  id,
			"tenant_id": tenantID,
		})
		return nil, err
	}

	// Check if model exists
	if model == nil {
		logger.Error(ctx, "Model not found")
		return nil, ErrModelNotFound
	}
	if err := s.validateApprovedModelEndpoint(ctx, model); err != nil {
		return nil, err
	}

	logger.Infof(ctx, "Model found, name: %s, status: %s", model.Name, model.Status)

	// Check model status
	if model.Status == types.ModelStatusActive {
		return model, nil
	}

	if model.Status == types.ModelStatusDownloading {
		logger.Warn(ctx, "Model is currently downloading")
		return nil, errors.New("model is currently downloading")
	}

	if model.Status == types.ModelStatusDownloadFailed {
		logger.Error(ctx, "Model download failed")
		return nil, errors.New("model download failed")
	}

	logger.Error(ctx, "Model status is abnormal")
	return nil, errors.New("abnormal model status")
}

// ListModels returns all models belonging to the tenant
func (s *modelService) ListModels(ctx context.Context) ([]*types.Model, error) {
	logger.Info(ctx, "Start listing models")

	tenantID := types.MustTenantIDFromContext(ctx)
	logger.Infof(ctx, "Listing models for tenant ID: %d", tenantID)

	// List models from repository with no additional filters
	models, err := s.repo.List(ctx, tenantID, "", "")
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"tenant_id": tenantID,
		})
		return nil, err
	}

	logger.Infof(ctx, "Retrieved %d models successfully", len(models))
	return models, nil
}

// UpdateModel updates an existing model in the repository
func (s *modelService) UpdateModel(ctx context.Context, model *types.Model) error {
	if model == nil {
		return errors.New("model is required")
	}
	logger.Info(ctx, "Start updating model")
	logger.Infof(ctx, "Updating model ID: %s, name: %s", model.ID, model.Name)

	// Check if the model is builtin - builtin models cannot be updated
	tenantID := types.MustTenantIDFromContext(ctx)
	existingModel, err := s.repo.GetByID(ctx, tenantID, model.ID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id": model.ID,
		})
		return err
	}
	if existingModel != nil && existingModel.IsBuiltin {
		logger.Warnf(ctx, "Attempted to update builtin model: %s", model.ID)
		return errors.New("builtin models cannot be updated")
	}
	normalizeModelParameters(model)
	if err := s.validateApprovedModelEndpoint(ctx, model); err != nil {
		return err
	}
	if err := validateDeclaredModelCapabilities(model); err != nil {
		return err
	}
	if airGappedMode() && (model.Parameters.Location == types.EndpointPublic || model.Parameters.Location == types.EndpointUnknown) {
		return errors.New("air-gapped mode rejects public model endpoints")
	}
	if existingModel != nil && model.Status == "" {
		model.Status = existingModel.Status
	}

	// Update model in repository
	err = s.repo.Update(ctx, model)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id":   model.ID,
			"model_name": model.Name,
		})
		return err
	}

	logger.Infof(ctx, "Model updated successfully: %s", model.ID)
	return nil
}

// DeleteModel removes a model from the repository
func (s *modelService) DeleteModel(ctx context.Context, id string) error {
	logger.Info(ctx, "Start deleting model")
	logger.Infof(ctx, "Deleting model ID: %s", id)

	tenantID := types.MustTenantIDFromContext(ctx)
	logger.Infof(ctx, "Tenant ID: %d", tenantID)

	// Check if the model is builtin - builtin models cannot be deleted
	existingModel, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id": id,
		})
		return err
	}
	if existingModel != nil && existingModel.IsBuiltin {
		logger.Warnf(ctx, "Attempted to delete builtin model: %s", id)
		return errors.New("builtin models cannot be deleted")
	}

	// Delete model from repository
	err = s.repo.Delete(ctx, tenantID, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id":  id,
			"tenant_id": tenantID,
		})
		return err
	}

	logger.Infof(ctx, "Model deleted successfully: %s", id)
	return nil
}

// GetEmbeddingModel retrieves and initializes an embedding model instance
// Takes a model ID and returns an Embedder interface implementation
func (s *modelService) GetEmbeddingModel(ctx context.Context, modelId string) (embedding.Embedder, error) {
	// Get the model details
	model, err := s.GetModelByID(ctx, modelId)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id": modelId,
		})
		return nil, err
	}
	if err := s.validateApprovedModelEndpoint(ctx, model); err != nil {
		return nil, err
	}

	logger.Infof(ctx, "Getting embedding model: %s, source: %s", model.Name, model.Source)

	appID, appSecret := s.resolveWeKnoraCloudCredentials(ctx, &model.Parameters)

	embeddingConfig := embedding.ConfigFromModel(model, appID, appSecret)
	embeddingConfig.ValidateIP, err = s.modelIPValidator(ctx, model)
	if err != nil {
		return nil, err
	}
	embedder, err := embedding.NewEmbedder(embeddingConfig, s.pooler, s.ollamaService)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id":   model.ID,
			"model_name": model.Name,
		})
		return nil, err
	}

	logger.Info(ctx, "Embedding model initialized successfully")
	return embedder, nil
}

// GetEmbeddingModelForTenant retrieves and initializes an embedding model for a specific tenant
// This is used for cross-tenant knowledge base sharing where the embedding model from
// the source tenant must be used to ensure vector compatibility
func (s *modelService) GetEmbeddingModelForTenant(ctx context.Context, modelId string, tenantID uint64) (embedding.Embedder, error) {
	// Check if model ID is empty
	if modelId == "" {
		logger.Error(ctx, "Model ID is empty")
		return nil, errors.New("model ID cannot be empty")
	}

	// Fetch model from repository using the specified tenant ID
	model, err := s.repo.GetByID(ctx, tenantID, modelId)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id":  modelId,
			"tenant_id": tenantID,
		})
		return nil, err
	}

	if model == nil {
		logger.Error(ctx, "Model not found for specified tenant")
		return nil, ErrModelNotFound
	}

	if model.Status != types.ModelStatusActive {
		logger.Errorf(ctx, "Model is not active, status: %s", model.Status)
		return nil, errors.New("model is not active")
	}
	if err := s.validateApprovedModelEndpoint(ctx, model); err != nil {
		return nil, err
	}

	logger.Infof(ctx, "Getting cross-tenant embedding model: %s, source: %s, tenant: %d", model.Name, model.Source, tenantID)

	appID, appSecret := s.resolveWeKnoraCloudCredentials(ctx, &model.Parameters)

	embeddingConfig := embedding.ConfigFromModel(model, appID, appSecret)
	embeddingConfig.ValidateIP, err = s.modelIPValidator(ctx, model)
	if err != nil {
		return nil, err
	}
	embedder, err := embedding.NewEmbedder(embeddingConfig, s.pooler, s.ollamaService)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id":   model.ID,
			"model_name": model.Name,
			"tenant_id":  tenantID,
		})
		return nil, err
	}

	logger.Info(ctx, "Cross-tenant embedding model initialized successfully")
	return embedder, nil
}

// GetRerankModel retrieves and initializes a reranking model instance
// Takes a model ID and returns a Reranker interface implementation
func (s *modelService) GetRerankModel(ctx context.Context, modelId string) (rerank.Reranker, error) {
	// Get the model details
	model, err := s.GetModelByID(ctx, modelId)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id": modelId,
		})
		return nil, err
	}
	if err := s.validateApprovedModelEndpoint(ctx, model); err != nil {
		return nil, err
	}

	logger.Infof(ctx, "Getting rerank model: %s, source: %s", model.Name, model.Source)

	appID, appSecret := s.resolveWeKnoraCloudCredentials(ctx, &model.Parameters)

	rerankConfig := rerank.ConfigFromModel(model, appID, appSecret)
	rerankConfig.ValidateIP, err = s.modelIPValidator(ctx, model)
	if err != nil {
		return nil, err
	}
	reranker, err := rerank.NewReranker(rerankConfig)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id":   model.ID,
			"model_name": model.Name,
		})
		return nil, err
	}

	logger.Info(ctx, "Rerank model initialized successfully")
	return reranker, nil
}

// GetChatModel retrieves and initializes a chat model instance
// Takes a model ID and returns a Chat interface implementation
func (s *modelService) GetChatModel(ctx context.Context, modelId string) (chat.Chat, error) {
	// Check if model ID is empty
	if modelId == "" {
		logger.Error(ctx, "Model ID is empty")
		return nil, errors.New("model ID cannot be empty")
	}

	tenantID := types.MustTenantIDFromContext(ctx)

	// Get the model directly from repository to avoid status checks
	model, err := s.repo.GetByID(ctx, tenantID, modelId)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id":  modelId,
			"tenant_id": tenantID,
		})
		return nil, err
	}

	if model == nil {
		logger.Error(ctx, "Chat model not found")
		return nil, ErrModelNotFound
	}
	if err := s.validateApprovedModelEndpoint(ctx, model); err != nil {
		return nil, err
	}

	logger.Infof(ctx, "Getting chat model: %s, source: %s", model.Name, model.Source)

	appID, appSecret := s.resolveWeKnoraCloudCredentials(ctx, &model.Parameters)

	chatConfig := chat.ConfigFromModel(model, appID, appSecret)
	chatConfig.ValidateIP, err = s.modelIPValidator(ctx, model)
	if err != nil {
		return nil, err
	}
	chatModel, err := chat.NewChat(chatConfig, s.ollamaService)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id":   model.ID,
			"model_name": model.Name,
		})
		return nil, err
	}

	return chatModel, nil
}

// GetVLMModel retrieves and initializes a vision language model instance.
func (s *modelService) GetVLMModel(ctx context.Context, modelId string) (vlm.VLM, error) {
	if modelId == "" {
		return nil, errors.New("model ID cannot be empty")
	}

	tenantID := types.MustTenantIDFromContext(ctx)

	model, err := s.repo.GetByID(ctx, tenantID, modelId)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id":  modelId,
			"tenant_id": tenantID,
		})
		return nil, err
	}

	if model == nil {
		return nil, ErrModelNotFound
	}
	if err := s.validateApprovedModelEndpoint(ctx, model); err != nil {
		return nil, err
	}

	logger.Infof(ctx, "Getting VLM model: %s, source: %s", model.Name, model.Source)

	appID, appSecret := s.resolveWeKnoraCloudCredentials(ctx, &model.Parameters)

	vlmConfig := vlm.ConfigFromModel(model, appID, appSecret)
	vlmConfig.ValidateIP, err = s.modelIPValidator(ctx, model)
	if err != nil {
		return nil, err
	}
	vlmModel, err := vlm.NewVLM(vlmConfig, s.ollamaService)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id":   model.ID,
			"model_name": model.Name,
		})
		return nil, err
	}

	return vlmModel, nil
}

// Note: default model selection logic has been removed; models no longer
// maintain a per-type default flag at the service layer.

// GetASRModel retrieves and initializes an automatic speech recognition model instance.
func (s *modelService) GetASRModel(ctx context.Context, modelId string) (asr.ASR, error) {
	if modelId == "" {
		return nil, errors.New("model ID cannot be empty")
	}

	tenantID := types.MustTenantIDFromContext(ctx)

	model, err := s.repo.GetByID(ctx, tenantID, modelId)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id":  modelId,
			"tenant_id": tenantID,
		})
		return nil, err
	}

	if model == nil {
		return nil, ErrModelNotFound
	}
	if err := s.validateApprovedModelEndpoint(ctx, model); err != nil {
		return nil, err
	}

	logger.Infof(ctx, "Getting ASR model: %s, source: %s", model.Name, model.Source)

	asrConfig := asr.ConfigFromModel(model)
	asrConfig.ValidateIP, err = s.modelIPValidator(ctx, model)
	if err != nil {
		return nil, err
	}
	sttModel, err := asr.NewASR(asrConfig)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id":   model.ID,
			"model_name": model.Name,
		})
		return nil, err
	}

	return sttModel, nil
}

// GetTTSModel retrieves an OpenAI-compatible text-to-speech model.
func (s *modelService) GetTTSModel(ctx context.Context, modelId string) (tts.TTS, error) {
	if modelId == "" {
		return nil, errors.New("model ID cannot be empty")
	}
	tenantID := types.MustTenantIDFromContext(ctx)
	model, err := s.repo.GetByID(ctx, tenantID, modelId)
	if err != nil {
		return nil, err
	}
	if model == nil {
		return nil, ErrModelNotFound
	}
	if err := s.validateApprovedModelEndpoint(ctx, model); err != nil {
		return nil, err
	}
	validator, err := s.modelIPValidator(ctx, model)
	if err != nil {
		return nil, err
	}
	return tts.NewOpenAITTS(tts.Config{BaseURL: model.Parameters.BaseURL, APIKey: model.Parameters.APIKey, ModelName: model.Name, ModelID: model.ID, Voice: modelDefaultTTSVoice(model), CustomHeaders: model.Parameters.CustomHeaders, ValidateIP: validator})
}

func modelDefaultTTSVoice(model *types.Model) string {
	if model == nil {
		return ""
	}
	if voice := strings.TrimSpace(model.Parameters.ExtraConfig["voice"]); voice != "" {
		return voice
	}
	return strings.TrimSpace(model.Parameters.ExtraConfig["voice_name"])
}

// ProbeModelCapabilities runs a bounded, auditable preflight without
// downloading weights. Network models are checked through the shared
// transport and local models are checked as preloaded Ollama instances.
func (s *modelService) probeLegacyModelCapabilities(ctx context.Context, modelID string) (*types.ModelPreflightResult, error) {
	model, err := s.GetModelByID(ctx, modelID)
	if err != nil {
		return nil, err
	}
	result := &types.ModelPreflightResult{ModelID: model.ID, ModelName: model.Name, Location: model.Parameters.Location, Protocol: model.Parameters.Protocol, CheckedAt: time.Now().UTC()}
	roles := append([]types.ModelRole(nil), model.Parameters.Capabilities.Roles...)
	if len(roles) == 0 {
		roles = []types.ModelRole{modelRoleForType(model.Type)}
	}
	newProbe := func(role types.ModelRole) types.ModelCapabilityProbeResult {
		return types.ModelCapabilityProbeResult{Role: role, ModelKey: model.ID, CheckedAt: result.CheckedAt}
	}
	if model.Source == types.ModelSourceLocal {
		status := types.CapabilityProbePassed
		reason := ""
		if s.ollamaService == nil {
			status, reason = types.CapabilityProbeMissingResource, "Ollama service is unavailable"
		} else if available, checkErr := s.ollamaService.IsModelAvailable(ctx, model.Name); checkErr != nil {
			status, reason = types.CapabilityProbeFailed, checkErr.Error()
		} else if !available {
			status, reason = types.CapabilityProbeMissingResource, "preloaded model is missing"
		}
		for _, role := range roles {
			probe := newProbe(role)
			probe.Status, probe.Error = status, reason
			if status == types.CapabilityProbePassed {
				if err := model.Parameters.Capabilities.ValidateRole(role); err != nil {
					probe.Status, probe.Error = types.CapabilityProbeUnsupported, err.Error()
				}
			}
			result.Probes = append(result.Probes, probe)
		}
		return result, nil
	}
	started := time.Now()
	base := strings.TrimRight(model.Parameters.BaseURL, "/")
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	req, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if requestErr != nil {
		for _, role := range roles {
			probe := newProbe(role)
			probe.Status, probe.Error = types.CapabilityProbeFailed, requestErr.Error()
			result.Probes = append(result.Probes, probe)
		}
		return result, nil
	}
	if model.Parameters.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+model.Parameters.APIKey)
	}
	transport.ApplyHeaders(req, model.Parameters.CustomHeaders)
	validateIP, validatorErr := s.modelIPValidator(ctx, model)
	if validatorErr != nil {
		for _, role := range roles {
			probe := newProbe(role)
			probe.Status, probe.Error = types.CapabilityProbeFailed, validatorErr.Error()
			result.Probes = append(result.Probes, probe)
		}
		return result, nil
	}
	endpointScheme, endpointName, endpointPort, endpointErr := types.NormalizeEndpoint(model.Parameters.BaseURL)
	if endpointErr != nil {
		for _, role := range roles {
			probe := newProbe(role)
			probe.Status, probe.Error = types.CapabilityProbeFailed, endpointErr.Error()
			result.Probes = append(result.Probes, probe)
		}
		return result, nil
	}
	resp, callErr := transport.NewHTTPClient(transport.Config{
		Timeout:      10 * time.Second,
		ValidateIP:   validateIP,
		AllowedHosts: []string{endpointHost(model.Parameters.BaseURL)},
		ValidateURL: func(target *url.URL) error {
			scheme, host, port, err := types.NormalizeEndpoint(target.String())
			if err != nil || scheme != endpointScheme || host != endpointName || port != endpointPort {
				return fmt.Errorf("model endpoint changed during request")
			}
			return nil
		},
	}).Do(req)
	latency := time.Since(started).Milliseconds()
	if callErr != nil {
		for _, role := range roles {
			probe := newProbe(role)
			probe.Status, probe.Error, probe.LatencyMs = types.CapabilityProbeFailed, callErr.Error(), latency
			result.Probes = append(result.Probes, probe)
		}
		return result, nil
	}
	defer resp.Body.Close()
	for _, role := range roles {
		probe := newProbe(role)
		probe.LatencyMs = latency
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			probe.Status, probe.Error = types.CapabilityProbeUnsupported, resp.Status
		} else if err := model.Parameters.Capabilities.ValidateRole(role); err != nil {
			probe.Status, probe.Error = types.CapabilityProbeUnsupported, err.Error()
		} else {
			probe.Status = types.CapabilityProbePassed
		}
		result.Probes = append(result.Probes, probe)
	}
	return result, nil
}
