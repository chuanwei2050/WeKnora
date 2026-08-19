package modelprofile

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type bootstrapRole struct {
	modelType types.ModelType
	modelRole types.ModelRole
	use       string
}

var bootstrapRoles = map[string]bootstrapRole{
	"chat":             {modelType: types.ModelTypeKnowledgeQA, modelRole: types.ModelRoleChat, use: "chat"},
	"verifier_1":       {modelType: types.ModelTypeVerifier, modelRole: types.ModelRoleVerifier, use: "verifier"},
	"verifier_2":       {modelType: types.ModelTypeVerifier, modelRole: types.ModelRoleVerifier, use: "verifier"},
	"evaluation_judge": {modelType: types.ModelTypeJudge, modelRole: types.ModelRoleEvaluationJudge, use: "judge"},
	"embedding":        {modelType: types.ModelTypeEmbedding, modelRole: types.ModelRoleEmbedding, use: "embedding"},
	"rerank":           {modelType: types.ModelTypeRerank, modelRole: types.ModelRoleRerank, use: "rerank"},
	"vlm":              {modelType: types.ModelTypeVLLM, modelRole: types.ModelRoleVLM, use: "vlm"},
	"asr":              {modelType: types.ModelTypeASR, modelRole: types.ModelRoleASR, use: "asr"},
	"tts":              {modelType: types.ModelTypeTTS, modelRole: types.ModelRoleTTS, use: "tts"},
}

const modelSeedVersion = 6

const legacyPrivateOpenAICompatibleProvider = "private-openai-compatible"

const (
	legacyOfflineLLMBaseURL = "http://114.242.58.129:30000"
	offlineLLMBaseURL       = legacyOfflineLLMBaseURL + "/v1"
	legacyOfflineLLMName    = "qwen3.8-27b"
	offlineLLMName          = "Qwen3.8-27B-FP8"
)

type seedStateRepository interface {
	GetPlatformSettings(ctx context.Context) (*types.PlatformSettings, error)
	UpdatePlatformSettings(ctx context.Context, settings *types.PlatformSettings) error
}

// BootstrapPlan resolves one profile into explicit role registrations.
func BootstrapPlan(profile types.ModelProfile) []*types.Model {
	prefix := "ONLINE"
	policy := types.ArtifactAllowDownload
	if profile == types.ModelProfileOffline {
		prefix = "OFFLINE"
		policy = types.ArtifactPreloadedOnly
	}
	provider := expandAllEnvRefs(os.Getenv(prefix + "_MODEL_PROVIDER"))

	models := make([]*types.Model, 0, len(roleSpecs))
	for _, spec := range roleSpecs {
		role := bootstrapRoles[spec.Role]
		name := strings.TrimSpace(expandAllEnvRefs(os.Getenv(prefix + "_" + spec.Stem + "_NAME")))
		baseURL := strings.TrimSpace(expandAllEnvRefs(os.Getenv(prefix + "_" + spec.Stem + "_BASE_URL")))
		if !validBootstrapConfig(name, baseURL) {
			continue
		}
		apiKey := expandAllEnvRefs(os.Getenv(prefix + "_" + spec.Stem + "_API_KEY"))
		if strings.Contains(apiKey, "__FILL_") || strings.Contains(apiKey, "${") {
			continue
		}

		source := types.ModelSource(strings.ToLower(strings.TrimSpace(expandAllEnvRefs(os.Getenv(prefix + "_" + spec.Stem + "_SOURCE")))))
		if source == "" {
			source = types.ModelSourceRemote
		}
		dimension := 0
		if spec.HasDimension {
			dimension, _ = strconv.Atoi(strings.TrimSpace(expandAllEnvRefs(os.Getenv(prefix + "_" + spec.Stem + "_DIMENSION"))))
		}

		location := types.DeriveEndpointLocation(baseURL, nil)
		params := types.ModelParameters{
			BaseURL:        baseURL,
			APIKey:         apiKey,
			Provider:       provider,
			Protocol:       types.ModelProtocolOpenAICompatible,
			Location:       location,
			ArtifactPolicy: policy,
			EndpointUse:    role.use,
			EmbeddingParameters: types.EmbeddingParameters{
				Dimension: dimension,
			},
			Capabilities: capabilityFor(role.modelRole, policy, location, dimension),
		}
		models = append(models, &types.Model{
			TenantID:    types.PlatformModelTenantID,
			Name:        name,
			Type:        role.modelType,
			Source:      source,
			Description: fmt.Sprintf("%s model managed by MODEL_PROFILE=%s", spec.Role, profile),
			Parameters:  params,
			IsDefault:   true,
			IsBuiltin:   false,
			Profile:     profile,
			ProfileRole: spec.Role,
			Status:      types.ModelStatusActive,
		})
	}
	return models
}

// Bootstrap seeds both profiles once and initializes the persisted active profile.
func Bootstrap(
	ctx context.Context,
	repo interfaces.ModelRepository,
	stateRepo seedStateRepository,
	approvedEndpoints interfaces.ApprovedEndpointRepository,
) error {
	settings, err := stateRepo.GetPlatformSettings(ctx)
	if err != nil {
		return fmt.Errorf("load model seed state: %w", err)
	}
	if _, ok := types.ParseModelProfile(string(settings.ModelProfile)); !ok {
		settings.ModelProfile = types.ModelProfile(ResolveProfile().Profile)
	}
	if settings.ModelSeedVersion >= modelSeedVersion {
		if err := stateRepo.UpdatePlatformSettings(ctx, settings); err != nil {
			return fmt.Errorf("save initial model profile: %w", err)
		}
		return nil
	}

	existing, err := repo.List(ctx, types.PlatformModelTenantID, "", "")
	if err != nil {
		return fmt.Errorf("list profile models: %w", err)
	}
	legacyModels := append([]*types.Model(nil), existing...)
	if settings.ModelSeedVersion < 5 {
		for _, model := range existing {
			if model != nil && model.Profile == types.ModelProfileOffline &&
				strings.Contains(model.Description, "managed by MODEL_PROFILE=offline") &&
				normalizedBaseURL(model.Parameters.BaseURL) == legacyOfflineLLMBaseURL {
				model.Parameters.BaseURL = offlineLLMBaseURL
				if err := repo.Update(ctx, model); err != nil {
					return fmt.Errorf("upgrade profile model base URL %q (%s): %w", model.Name, model.Type, err)
				}
			}
		}
	}
	if settings.ModelSeedVersion < 6 {
		for _, model := range existing {
			if model != nil && model.Profile == types.ModelProfileOffline &&
				strings.Contains(model.Description, "managed by MODEL_PROFILE=offline") &&
				model.Name == legacyOfflineLLMName {
				model.Name = offlineLLMName
				if err := repo.Update(ctx, model); err != nil {
					return fmt.Errorf("upgrade profile model name %q (%s): %w", model.Name, model.Type, err)
				}
			}
		}
	}
	for _, profile := range []types.ModelProfile{types.ModelProfileOnline, types.ModelProfileOffline} {
		for _, candidate := range BootstrapPlan(profile) {
			matched := findEquivalentModel(existing, candidate)
			if settings.ModelSeedVersion >= 2 {
				if matched == nil {
					continue
				}
				changed := false
				if err := bindApprovedProfileEndpoint(ctx, approvedEndpoints, candidate); err != nil {
					return fmt.Errorf("approve profile model endpoint %q (%s): %w", candidate.Name, candidate.Type, err)
				}
				if candidate.Parameters.ApprovedEndpointID != "" &&
					matched.Parameters.ApprovedEndpointID != candidate.Parameters.ApprovedEndpointID {
					matched.Parameters.ApprovedEndpointID = candidate.Parameters.ApprovedEndpointID
					changed = true
				}
				if settings.ModelSeedVersion < 4 && matched.Parameters.Provider == legacyPrivateOpenAICompatibleProvider {
					matched.Parameters.Provider = "generic"
					changed = true
				}
				if changed {
					if err := repo.Update(ctx, matched); err != nil {
						return fmt.Errorf("upgrade profile model %q (%s): %w", matched.Name, matched.Type, err)
					}
				}
				continue
			}
			if err := bindApprovedProfileEndpoint(ctx, approvedEndpoints, candidate); err != nil {
				return fmt.Errorf("approve profile model endpoint %q (%s): %w", candidate.Name, candidate.Type, err)
			}
			candidate.IsDefault = profile == settings.ModelProfile
			if matched != nil {
				if matched.IsBuiltin && !strings.Contains(matched.Description, "managed by MODEL_PROFILE=") {
					matched = nil
				} else {
					changed := false
					if matched.Profile == "" {
						matched.Profile = profile
						matched.ProfileRole = candidate.ProfileRole
						changed = true
					}
					if matched.IsBuiltin && strings.Contains(matched.Description, "managed by MODEL_PROFILE=") {
						matched.IsBuiltin = false
						changed = true
					}
					if changed {
						if err := repo.Update(ctx, matched); err != nil {
							return fmt.Errorf("classify profile seed %q (%s): %w", matched.Name, matched.Type, err)
						}
					}
					continue
				}
			}
			if err := repo.Create(ctx, candidate); err != nil {
				return fmt.Errorf("create profile model %q (%s): %w", candidate.Name, candidate.Type, err)
			}
			if candidate.IsDefault {
				if err := repo.ClearDefaultByType(ctx, uint(types.PlatformModelTenantID), candidate.Type, candidate.Profile, candidate.ProfileRole, candidate.ID); err != nil {
					return fmt.Errorf("set profile model default %q (%s): %w", candidate.Name, candidate.Type, err)
				}
			}
			existing = append(existing, candidate)
		}
	}
	for _, model := range legacyModels {
		if model == nil || model.Profile != "" {
			continue
		}
		model.Profile = settings.ModelProfile
		model.ProfileRole = profileRoleForModelType(model.Type)
		if err := repo.Update(ctx, model); err != nil {
			return fmt.Errorf("classify legacy model %q (%s): %w", model.Name, model.Type, err)
		}
	}
	settings.ModelSeedVersion = modelSeedVersion
	if err := stateRepo.UpdatePlatformSettings(ctx, settings); err != nil {
		return fmt.Errorf("save model seed state: %w", err)
	}
	return nil
}

func bindApprovedProfileEndpoint(
	ctx context.Context,
	repo interfaces.ApprovedEndpointRepository,
	model *types.Model,
) error {
	if repo == nil || model == nil || model.Profile != types.ModelProfileOffline ||
		model.Source == types.ModelSourceLocal || model.Parameters.BaseURL == "" {
		return nil
	}
	scheme, host, port, err := types.NormalizeEndpoint(model.Parameters.BaseURL)
	if err != nil {
		return err
	}
	items, err := repo.List(ctx, types.PlatformScopeTenantID, types.EndpointCategoryModel)
	if err != nil {
		return err
	}
	use := model.Parameters.EndpointUse
	if use == "" {
		use = "model"
	}
	role := bootstrapRoles[model.ProfileRole].modelRole
	for _, endpoint := range items {
		if endpoint == nil || !strings.EqualFold(endpoint.Scheme, scheme) ||
			!strings.EqualFold(endpoint.Host, host) || endpoint.Port != port {
			continue
		}
		changed := appendUniqueString(&endpoint.AllowedUses, use)
		changed = appendUniqueModelRole(&endpoint.AllowedModelRoles, role) || changed
		if changed {
			if err := repo.Update(ctx, endpoint); err != nil {
				return err
			}
		}
		model.Parameters.ApprovedEndpointID = endpoint.ID
		return nil
	}
	endpoint := &types.ApprovedEndpoint{
		TenantID:          types.PlatformScopeTenantID,
		Scheme:            scheme,
		Host:              host,
		Port:              port,
		Protocol:          string(types.ModelProtocolOpenAICompatible),
		TLSRequired:       scheme == "https",
		Category:          types.EndpointCategoryModel,
		AllowedUses:       types.StringArray{use},
		AllowedModelRoles: types.ModelRoleArray{role},
		CreatedBy:         "model-profile-bootstrap",
	}
	if err := repo.Create(ctx, endpoint); err != nil {
		return err
	}
	model.Parameters.ApprovedEndpointID = endpoint.ID
	return nil
}

func appendUniqueString(values *types.StringArray, candidate string) bool {
	for _, value := range *values {
		if strings.EqualFold(strings.TrimSpace(value), candidate) {
			return false
		}
	}
	*values = append(*values, candidate)
	return true
}

func appendUniqueModelRole(values *types.ModelRoleArray, candidate types.ModelRole) bool {
	for _, value := range *values {
		if value == candidate {
			return false
		}
	}
	*values = append(*values, candidate)
	return true
}

func profileRoleForModelType(modelType types.ModelType) string {
	switch modelType {
	case types.ModelTypeKnowledgeQA:
		return "chat"
	case types.ModelTypeVerifier:
		return "verifier_2"
	case types.ModelTypeJudge:
		return "evaluation_judge"
	case types.ModelTypeEmbedding:
		return "embedding"
	case types.ModelTypeRerank:
		return "rerank"
	case types.ModelTypeVLM, types.ModelTypeVLLM:
		return "vlm"
	case types.ModelTypeASR:
		return "asr"
	case types.ModelTypeTTS:
		return "tts"
	default:
		return ""
	}
}

func expandAllEnvRefs(value string) string {
	current := value
	for i := 0; i < 8; i++ {
		next := envRefPattern.ReplaceAllStringFunc(current, func(match string) string {
			return os.Getenv(match[2 : len(match)-1])
		})
		if next == current {
			return strings.TrimSpace(next)
		}
		current = next
	}
	return strings.TrimSpace(current)
}

func validBootstrapConfig(name, baseURL string) bool {
	if isMissingEnvValue(name, baseURL) || strings.TrimSpace(baseURL) == "" {
		return false
	}
	parsed, err := url.Parse(baseURL)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func findEquivalentModel(models []*types.Model, candidate *types.Model) *types.Model {
	for _, model := range models {
		if model != nil && strings.TrimSpace(model.Name) == strings.TrimSpace(candidate.Name) &&
			model.Type == candidate.Type && normalizedBaseURL(model.Parameters.BaseURL) == normalizedBaseURL(candidate.Parameters.BaseURL) &&
			(model.Profile == "" || (model.Profile == candidate.Profile && model.ProfileRole == candidate.ProfileRole)) {
			return model
		}
	}
	return nil
}

func normalizedBaseURL(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

func capabilityFor(
	role types.ModelRole,
	policy types.ArtifactPolicy,
	location types.EndpointLocation,
	dimension int,
) types.ModelCapabilityManifest {
	capability := types.ModelCapabilityManifest{
		Roles:              []types.ModelRole{role},
		Protocol:           types.ModelProtocolOpenAICompatible,
		Location:           location,
		ArtifactPolicy:     policy,
		EmbeddingDimension: dimension,
	}
	switch role {
	case types.ModelRoleChat:
		capability.Streaming = true
	case types.ModelRoleVerifier, types.ModelRoleEvaluationJudge:
		capability.StructuredOutput = true
	case types.ModelRoleVLM:
		capability.Roles = []types.ModelRole{types.ModelRoleChat, types.ModelRoleVLM}
		capability.Streaming = true
		capability.VisionInput = true
	case types.ModelRoleASR:
		capability.AudioInput = true
	case types.ModelRoleTTS:
		capability.AudioOutput = true
	}
	return capability
}
