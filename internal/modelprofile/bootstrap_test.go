package modelprofile

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestBootstrapPlanBuildsExplicitOfflineRoleModels(t *testing.T) {
	setBootstrapTestEnv(t)

	models := BootstrapPlan(types.ModelProfileOffline)
	if len(models) != 7 {
		t.Fatalf("expected 7 registrations, got %d: %+v", len(models), models)
	}
	assertBootstrapModel(t, models, "Qwen3.8-27B-FP8", types.ModelTypeKnowledgeQA, "http://114.242.58.129:30000/v1")
	assertBootstrapModel(t, models, "Qwen3.8-27B-FP8", types.ModelTypeVerifier, "http://114.242.58.129:30000/v1")
	assertProfileRole(t, models, types.ModelProfileOffline, types.ModelProfileRoleQueryUnderstand, "qwen3.5-9b")
	assertBootstrapModel(t, models, "qwen3.5-9b", types.ModelTypeVerifier, "http://192.168.10.232:8003/v1")
	assertBootstrapModel(t, models, "Qwen3.8-27B-FP8", types.ModelTypeJudge, "http://114.242.58.129:30000/v1")
	embedding := assertBootstrapModel(t, models, "qwen3-embedding-4b", types.ModelTypeEmbedding, "http://192.168.10.232:8001/v1")
	if embedding.Parameters.EmbeddingParameters.Dimension != 2560 || embedding.Parameters.Capabilities.EmbeddingDimension != 2560 {
		t.Fatalf("embedding dimension mismatch: %+v", embedding.Parameters)
	}
	if embedding.Parameters.EmbeddingParameters.CompatibilityID != "qwen3-embedding-4b-v1" {
		t.Fatalf("embedding compatibility ID mismatch: %+v", embedding.Parameters)
	}
	if embedding.Parameters.APIKey != "profile-secret" {
		t.Fatal("referenced API key was not expanded")
	}
	vlm := assertBootstrapModel(t, models, "Qwen3.8-27B-FP8", types.ModelTypeVLLM, "http://114.242.58.129:30000/v1")
	if !vlm.Parameters.Capabilities.VisionInput {
		t.Fatal("VLM registration must declare vision input")
	}
}

func TestBootstrapIsIdempotent(t *testing.T) {
	setBootstrapTestEnv(t)
	repo := &bootstrapRepository{}
	stateRepo := &bootstrapStateRepository{settings: &types.PlatformSettings{ID: 1}}

	if err := Bootstrap(context.Background(), repo, stateRepo, nil); err != nil {
		t.Fatal(err)
	}
	if len(repo.models) != 7 || repo.clearCalls != 7 {
		t.Fatalf("first bootstrap models=%d clearCalls=%d", len(repo.models), repo.clearCalls)
	}
	if stateRepo.settings.ModelSeedVersion != modelSeedVersion {
		t.Fatalf("seed version=%d", stateRepo.settings.ModelSeedVersion)
	}
	if stateRepo.settings.ModelProfile != types.ModelProfileOffline {
		t.Fatalf("initial profile=%q, want offline from MODEL_PROFILE", stateRepo.settings.ModelProfile)
	}
	if err := Bootstrap(context.Background(), repo, stateRepo, nil); err != nil {
		t.Fatal(err)
	}
	if len(repo.models) != 7 || repo.clearCalls != 7 {
		t.Fatalf("second bootstrap created duplicates: models=%d clearCalls=%d", len(repo.models), repo.clearCalls)
	}
}

func TestBootstrapSeedsOnlineAndOfflineProfilesIndependently(t *testing.T) {
	setBootstrapTestEnv(t)
	t.Setenv("ONLINE_MODEL_PROVIDER", "SiliconFlow")
	t.Setenv("ONLINE_LLM_MODEL_NAME", "Qwen/Qwen3.6-27B")
	t.Setenv("ONLINE_LLM_MODEL_BASE_URL", "https://api.siliconflow.cn/v1")
	t.Setenv("ONLINE_VERIFIER_MODEL_2_NAME", "Qwen/Qwen3.5-9B")
	t.Setenv("ONLINE_VERIFIER_MODEL_2_BASE_URL", "https://api.siliconflow.cn/v1")
	t.Setenv("ONLINE_EMBEDDING_MODEL_NAME", "Qwen/Qwen3-Embedding-4B")
	t.Setenv("ONLINE_EMBEDDING_MODEL_BASE_URL", "https://api.siliconflow.cn/v1")
	t.Setenv("ONLINE_EMBEDDING_MODEL_DIMENSION", "2560")
	repo := &bootstrapRepository{}
	stateRepo := &bootstrapStateRepository{settings: &types.PlatformSettings{ID: 1}}

	if err := Bootstrap(context.Background(), repo, stateRepo, nil); err != nil {
		t.Fatal(err)
	}
	if len(repo.models) != 10 {
		t.Fatalf("seeded models=%d, want 7 offline + 3 online", len(repo.models))
	}
	assertProfileRole(t, repo.models, types.ModelProfileOnline, "chat", "Qwen/Qwen3.6-27B")
	assertProfileRole(t, repo.models, types.ModelProfileOnline, "verifier_2", "Qwen/Qwen3.5-9B")
	assertProfileRole(t, repo.models, types.ModelProfileOffline, "verifier_1", "qwen3.5-9b")
	assertProfileRole(t, repo.models, types.ModelProfileOffline, "verifier_2", "Qwen3.8-27B-FP8")
	assertProfileRole(t, repo.models, types.ModelProfileOffline, "evaluation_judge", "Qwen3.8-27B-FP8")
}

func TestBootstrapSeedsEditableModelsAndDoesNotRestoreEnvValues(t *testing.T) {
	setBootstrapTestEnv(t)
	repo := &bootstrapRepository{}
	stateRepo := &bootstrapStateRepository{settings: &types.PlatformSettings{ID: 1}}

	if err := Bootstrap(context.Background(), repo, stateRepo, nil); err != nil {
		t.Fatal(err)
	}
	for _, model := range repo.models {
		if model.IsBuiltin {
			t.Fatalf("seed model must be editable: %+v", model)
		}
	}
	repo.models[0].Name = "admin-edited-model"
	repo.models = repo.models[:len(repo.models)-1]

	if err := Bootstrap(context.Background(), repo, stateRepo, nil); err != nil {
		t.Fatal(err)
	}
	if len(repo.models) != 6 || repo.models[0].Name != "admin-edited-model" {
		t.Fatalf("restart restored env seeds: %+v", repo.models)
	}
}

func TestBootstrapUpgradeDoesNotReplaceAdministratorAuxiliaryRole(t *testing.T) {
	setBootstrapTestEnv(t)
	adminModel := &types.Model{
		ID:          "admin-query-model",
		Name:        "administrator-selected-model",
		Type:        types.ModelTypeVerifier,
		Profile:     types.ModelProfileOffline,
		ProfileRole: types.ModelProfileRoleQueryUnderstand,
		Status:      types.ModelStatusActive,
		IsDefault:   true,
	}
	repo := &bootstrapRepository{models: []*types.Model{adminModel}}
	stateRepo := &bootstrapStateRepository{settings: &types.PlatformSettings{
		ID: 1, ModelSeedVersion: 8, ModelProfile: types.ModelProfileOffline,
	}}

	if err := Bootstrap(context.Background(), repo, stateRepo, nil); err != nil {
		t.Fatal(err)
	}
	if len(repo.models) != 1 || repo.models[0] != adminModel || repo.models[0].Name != "administrator-selected-model" {
		t.Fatalf("env seed replaced persisted auxiliary role: %+v", repo.models)
	}
}

func TestBootstrapMakesExistingProfileSeedsEditable(t *testing.T) {
	setBootstrapTestEnv(t)
	models := BootstrapPlan(types.ModelProfileOffline)
	for index, model := range models {
		model.ID = fmt.Sprintf("existing-%d", index)
		model.IsBuiltin = true
	}
	repo := &bootstrapRepository{models: models}
	stateRepo := &bootstrapStateRepository{settings: &types.PlatformSettings{ID: 1}}

	if err := Bootstrap(context.Background(), repo, stateRepo, nil); err != nil {
		t.Fatal(err)
	}
	for _, model := range repo.models {
		if model.IsBuiltin {
			t.Fatalf("existing profile seed remained builtin: %+v", model)
		}
	}
}

func TestBootstrapAssignsLegacyModelsToInitialProfile(t *testing.T) {
	setBootstrapTestEnv(t)
	legacy := &types.Model{
		ID:   "legacy-rerank",
		Name: "custom-rerank",
		Type: types.ModelTypeRerank,
	}
	repo := &bootstrapRepository{models: []*types.Model{legacy}}
	stateRepo := &bootstrapStateRepository{settings: &types.PlatformSettings{ID: 1}}

	if err := Bootstrap(context.Background(), repo, stateRepo, nil); err != nil {
		t.Fatal(err)
	}
	if legacy.Profile != types.ModelProfileOffline || legacy.ProfileRole != "rerank" {
		t.Fatalf("legacy model classification = %q/%q", legacy.Profile, legacy.ProfileRole)
	}
}

func TestBootstrapBindsOfflineSeedsToApprovedEndpoints(t *testing.T) {
	setBootstrapTestEnv(t)
	repo := &bootstrapRepository{}
	stateRepo := &bootstrapStateRepository{settings: &types.PlatformSettings{ID: 1}}
	approvedRepo := &bootstrapApprovedEndpointRepository{}

	if err := Bootstrap(context.Background(), repo, stateRepo, approvedRepo); err != nil {
		t.Fatal(err)
	}
	if len(approvedRepo.items) != 3 {
		t.Fatalf("approved endpoints=%d, want 3 unique offline destinations", len(approvedRepo.items))
	}
	for _, model := range repo.models {
		if model.Profile == types.ModelProfileOffline && model.Source == types.ModelSourceRemote &&
			model.Parameters.ApprovedEndpointID == "" {
			t.Fatalf("offline seed has no approved endpoint: %+v", model)
		}
	}
	mainEndpoint := approvedRepo.find("114.242.58.129", 30000)
	if mainEndpoint == nil || len(mainEndpoint.AllowedUses) != 4 || len(mainEndpoint.AllowedModelRoles) != 4 {
		t.Fatalf("main endpoint permissions were not merged: %+v", mainEndpoint)
	}
	verifierEndpoint := approvedRepo.find("192.168.10.232", 8003)
	if verifierEndpoint == nil {
		t.Fatal("missing verifier approved endpoint")
	}
	if err := verifierEndpoint.ValidateConnection(
		"http://192.168.10.232:8003/v1",
		types.EndpointCategoryModel,
		"verifier",
		[]net.IP{net.ParseIP("192.168.10.232")},
		false,
	); err != nil {
		t.Fatalf("seed endpoint did not pass exact target validation: %v", err)
	}
	if err := verifierEndpoint.ValidateConnection(
		"http://192.168.10.232:9003/v1",
		types.EndpointCategoryModel,
		"verifier",
		[]net.IP{net.ParseIP("192.168.10.232")},
		false,
	); err == nil {
		t.Fatal("approved endpoint accepted a different port")
	}
}

func TestBootstrapUpgradesUnchangedExistingSeeds(t *testing.T) {
	setBootstrapTestEnv(t)
	models := BootstrapPlan(types.ModelProfileOffline)
	for index, model := range models {
		model.ID = fmt.Sprintf("existing-%d", index)
		model.Parameters.Provider = legacyPrivateOpenAICompatibleProvider
		if model.Parameters.BaseURL == offlineLLMBaseURL {
			model.Parameters.BaseURL = legacyOfflineLLMBaseURL
		}
		if model.Name == offlineLLMName {
			model.Name = legacyOfflineLLMName
		}
		if model.ProfileRole == "embedding" {
			model.Parameters.EmbeddingParameters.CompatibilityID = ""
		}
	}
	repo := &bootstrapRepository{models: models}
	stateRepo := &bootstrapStateRepository{settings: &types.PlatformSettings{
		ID: 1, ModelSeedVersion: 2, ModelProfile: types.ModelProfileOffline,
	}}
	approvedRepo := &bootstrapApprovedEndpointRepository{}

	if err := Bootstrap(context.Background(), repo, stateRepo, approvedRepo); err != nil {
		t.Fatal(err)
	}
	if len(repo.models) != len(models) {
		t.Fatalf("seed upgrade changed model count: %d != %d", len(repo.models), len(models))
	}
	for _, model := range repo.models {
		if model.Parameters.ApprovedEndpointID == "" {
			t.Fatalf("existing seed was not bound: %+v", model)
		}
		if model.Parameters.Provider != "generic" {
			t.Fatalf("legacy provider was not normalized: %+v", model)
		}
		if model.Parameters.BaseURL == legacyOfflineLLMBaseURL {
			t.Fatalf("legacy base URL was not normalized: %+v", model)
		}
		if model.Name == legacyOfflineLLMName {
			t.Fatalf("legacy model name was not normalized: %+v", model)
		}
		if model.ProfileRole == "embedding" && model.Parameters.EmbeddingParameters.CompatibilityID != "qwen3-embedding-4b-v1" {
			t.Fatalf("embedding compatibility ID was not seeded: %+v", model)
		}
	}
	if stateRepo.settings.ModelSeedVersion != modelSeedVersion {
		t.Fatalf("seed version=%d, want %d", stateRepo.settings.ModelSeedVersion, modelSeedVersion)
	}
}

func TestBootstrapDoesNotMutateUnrelatedBuiltinModel(t *testing.T) {
	setBootstrapTestEnv(t)
	builtin := BootstrapPlan(types.ModelProfileOffline)[0]
	builtin.ID = "platform-builtin"
	builtin.IsBuiltin = true
	builtin.Description = "platform managed model"
	repo := &bootstrapRepository{models: []*types.Model{builtin}}
	stateRepo := &bootstrapStateRepository{settings: &types.PlatformSettings{ID: 1}}

	if err := Bootstrap(context.Background(), repo, stateRepo, nil); err != nil {
		t.Fatal(err)
	}
	if !builtin.IsBuiltin {
		t.Fatal("unrelated builtin model was made editable")
	}
	if len(repo.models) != 8 {
		t.Fatalf("expected an editable seed alongside the builtin model, got %d models", len(repo.models))
	}
}

func TestFullOfflineBootstrapPlanSatisfiesChecklist(t *testing.T) {
	setBootstrapTestEnv(t)
	t.Setenv("OFFLINE_RERANK_MODEL_NAME", "bge-reranker-v2-m3")
	t.Setenv("OFFLINE_RERANK_MODEL_SOURCE", "remote")
	t.Setenv("OFFLINE_RERANK_MODEL_BASE_URL", "http://192.168.10.232:8002/v1")
	t.Setenv("OFFLINE_ASR_MODEL_NAME", "sensevoice-small")
	t.Setenv("OFFLINE_ASR_MODEL_SOURCE", "remote")
	t.Setenv("OFFLINE_ASR_MODEL_BASE_URL", "http://192.168.10.232:8004/v1")
	t.Setenv("OFFLINE_TTS_MODEL_NAME", "cosyvoice2-0.5b")
	t.Setenv("OFFLINE_TTS_MODEL_SOURCE", "remote")
	t.Setenv("OFFLINE_TTS_MODEL_BASE_URL", "http://192.168.10.232:8005/v1")

	models := BootstrapPlan(types.ModelProfileOffline)
	if len(models) != 10 {
		t.Fatalf("expected 10 registrations for 10 roles, got %d", len(models))
	}
	views := make([]ModelView, 0, len(models))
	for index, model := range models {
		views = append(views, ModelView{
			ID:                 fmt.Sprintf("model-%d", index),
			Name:               model.Name,
			Type:               string(model.Type),
			EmbeddingDimension: model.Parameters.EmbeddingParameters.Dimension,
		})
	}
	status := Build(views)
	if status.Summary.OK != 10 || status.Summary.MissingEnv != 0 || status.Summary.MissingRegistration != 0 || status.Summary.Mismatch != 0 {
		t.Fatalf("bootstrap plan does not satisfy checklist: %+v", status.Summary)
	}
}

func TestBootstrapSkipsInvalidConfig(t *testing.T) {
	clearOnlineOfflineEnv(t)
	t.Setenv("MODEL_PROFILE", "offline")
	t.Setenv("OFFLINE_LLM_MODEL_NAME", "bad")
	t.Setenv("OFFLINE_LLM_MODEL_BASE_URL", "http://__FILL_HOST__:8000/v1")
	t.Setenv("OFFLINE_EMBEDDING_MODEL_NAME", "embedding")
	t.Setenv("OFFLINE_EMBEDDING_MODEL_BASE_URL", "not-a-url")
	if models := BootstrapPlan(types.ModelProfileOffline); len(models) != 0 {
		t.Fatalf("invalid configs must be skipped: %+v", models)
	}
}

func setBootstrapTestEnv(t *testing.T) {
	t.Helper()
	clearOnlineOfflineEnv(t)
	t.Setenv("MODEL_PROFILE", "offline")
	t.Setenv("OFFLINE_MODEL_PROVIDER", "generic")
	t.Setenv("OFFLINE_MODEL_API_KEY", "profile-secret")
	t.Setenv("OFFLINE_LLM_MODEL_NAME", "Qwen3.8-27B-FP8")
	t.Setenv("OFFLINE_LLM_MODEL_SOURCE", "remote")
	t.Setenv("OFFLINE_LLM_MODEL_BASE_URL", "http://114.242.58.129:30000/v1")
	t.Setenv("OFFLINE_LLM_MODEL_API_KEY", "${OFFLINE_MODEL_API_KEY}")
	t.Setenv("OFFLINE_VERIFIER_MODEL_1_NAME", "qwen3.5-9b")
	t.Setenv("OFFLINE_VERIFIER_MODEL_1_SOURCE", "remote")
	t.Setenv("OFFLINE_VERIFIER_MODEL_1_BASE_URL", "http://192.168.10.232:8003/v1")
	t.Setenv("OFFLINE_VERIFIER_MODEL_2_NAME", "${OFFLINE_LLM_MODEL_NAME}")
	t.Setenv("OFFLINE_VERIFIER_MODEL_2_SOURCE", "${OFFLINE_LLM_MODEL_SOURCE}")
	t.Setenv("OFFLINE_VERIFIER_MODEL_2_BASE_URL", "${OFFLINE_LLM_MODEL_BASE_URL}")
	t.Setenv("OFFLINE_EVALUATION_JUDGE_MODEL_NAME", "${OFFLINE_VERIFIER_MODEL_2_NAME}")
	t.Setenv("OFFLINE_EVALUATION_JUDGE_MODEL_SOURCE", "${OFFLINE_VERIFIER_MODEL_2_SOURCE}")
	t.Setenv("OFFLINE_EVALUATION_JUDGE_MODEL_BASE_URL", "${OFFLINE_VERIFIER_MODEL_2_BASE_URL}")
	t.Setenv("OFFLINE_EMBEDDING_MODEL_NAME", "qwen3-embedding-4b")
	t.Setenv("OFFLINE_EMBEDDING_MODEL_SOURCE", "remote")
	t.Setenv("OFFLINE_EMBEDDING_MODEL_BASE_URL", "http://192.168.10.232:8001/v1")
	t.Setenv("OFFLINE_EMBEDDING_MODEL_API_KEY", "${OFFLINE_MODEL_API_KEY}")
	t.Setenv("OFFLINE_EMBEDDING_MODEL_DIMENSION", "2560")
	t.Setenv("OFFLINE_EMBEDDING_MODEL_COMPATIBILITY_ID", "qwen3-embedding-4b-v1")
	t.Setenv("OFFLINE_RERANK_MODEL_NAME", "rerank")
	t.Setenv("OFFLINE_RERANK_MODEL_BASE_URL", "http://__FILL_HOST__:8002/v1")
	t.Setenv("OFFLINE_VLM_MODEL_NAME", "${OFFLINE_LLM_MODEL_NAME}")
	t.Setenv("OFFLINE_VLM_MODEL_SOURCE", "${OFFLINE_LLM_MODEL_SOURCE}")
	t.Setenv("OFFLINE_VLM_MODEL_BASE_URL", "${OFFLINE_LLM_MODEL_BASE_URL}")
	t.Setenv("OFFLINE_ASR_MODEL_NAME", "asr")
	t.Setenv("OFFLINE_ASR_MODEL_BASE_URL", "not-a-url")
}

func assertBootstrapModel(
	t *testing.T,
	models []*types.Model,
	name string,
	modelType types.ModelType,
	baseURL string,
) *types.Model {
	t.Helper()
	for _, model := range models {
		if model.Name == name && model.Type == modelType && model.Parameters.BaseURL == baseURL {
			return model
		}
	}
	t.Fatalf("missing model name=%s type=%s baseURL=%s", name, modelType, baseURL)
	return nil
}

func assertProfileRole(
	t *testing.T, models []*types.Model, profile types.ModelProfile, role, name string,
) {
	t.Helper()
	for _, model := range models {
		if model.Profile == profile && model.ProfileRole == role && model.Name == name {
			return
		}
	}
	t.Fatalf("missing profile=%s role=%s name=%s", profile, role, name)
}

type bootstrapRepository struct {
	models     []*types.Model
	clearCalls int
}

type bootstrapStateRepository struct {
	settings *types.PlatformSettings
}

type bootstrapApprovedEndpointRepository struct {
	items []*types.ApprovedEndpoint
}

func (r *bootstrapApprovedEndpointRepository) Create(_ context.Context, endpoint *types.ApprovedEndpoint) error {
	endpoint.ID = fmt.Sprintf("endpoint-%d", len(r.items)+1)
	r.items = append(r.items, endpoint)
	return nil
}

func (r *bootstrapApprovedEndpointRepository) GetByID(
	_ context.Context, _ uint64, id string,
) (*types.ApprovedEndpoint, error) {
	for _, endpoint := range r.items {
		if endpoint.ID == id {
			return endpoint, nil
		}
	}
	return nil, nil
}

func (r *bootstrapApprovedEndpointRepository) List(
	_ context.Context, _ uint64, _ types.ApprovedEndpointCategory,
) ([]*types.ApprovedEndpoint, error) {
	return r.items, nil
}

func (r *bootstrapApprovedEndpointRepository) Update(_ context.Context, _ *types.ApprovedEndpoint) error {
	return nil
}

func (r *bootstrapApprovedEndpointRepository) Delete(_ context.Context, _ uint64, _ string) error {
	return nil
}

func (r *bootstrapApprovedEndpointRepository) CreateAudit(
	_ context.Context, _ *types.ApprovedEndpointAudit,
) error {
	return nil
}

func (r *bootstrapApprovedEndpointRepository) ListAudits(
	_ context.Context, _ uint64, _ string,
) ([]*types.ApprovedEndpointAudit, error) {
	return nil, nil
}

func (r *bootstrapApprovedEndpointRepository) find(host string, port int) *types.ApprovedEndpoint {
	for _, endpoint := range r.items {
		if endpoint.Host == host && endpoint.Port == port {
			return endpoint
		}
	}
	return nil
}

func (r *bootstrapStateRepository) GetPlatformSettings(_ context.Context) (*types.PlatformSettings, error) {
	return r.settings, nil
}

func (r *bootstrapStateRepository) UpdatePlatformSettings(
	_ context.Context,
	settings *types.PlatformSettings,
) error {
	r.settings = settings
	return nil
}

func (r *bootstrapRepository) Create(_ context.Context, model *types.Model) error {
	model.ID = fmt.Sprintf("model-%d", len(r.models)+1)
	r.models = append(r.models, model)
	return nil
}

func (r *bootstrapRepository) GetByID(_ context.Context, _ uint64, id string) (*types.Model, error) {
	for _, model := range r.models {
		if model.ID == id {
			return model, nil
		}
	}
	return nil, nil
}

func (r *bootstrapRepository) List(
	_ context.Context,
	_ uint64,
	modelType types.ModelType,
	source types.ModelSource,
) ([]*types.Model, error) {
	var models []*types.Model
	for _, model := range r.models {
		if (modelType == "" || model.Type == modelType) && (source == "" || model.Source == source) {
			models = append(models, model)
		}
	}
	return models, nil
}

func (r *bootstrapRepository) Update(_ context.Context, model *types.Model) error {
	for index, existing := range r.models {
		if existing.ID == model.ID {
			r.models[index] = model
			return nil
		}
	}
	return nil
}

func (r *bootstrapRepository) Delete(_ context.Context, _ uint64, id string) error {
	for index, model := range r.models {
		if model.ID == id {
			r.models = append(r.models[:index], r.models[index+1:]...)
			break
		}
	}
	return nil
}

func (r *bootstrapRepository) ClearDefaultByType(
	_ context.Context,
	_ uint,
	modelType types.ModelType,
	profile types.ModelProfile,
	profileRole string,
	excludeID string,
) error {
	r.clearCalls++
	for _, model := range r.models {
		if model.Type == modelType && model.Profile == profile && model.ProfileRole == profileRole && model.ID != excludeID {
			model.IsDefault = false
		}
	}
	return nil
}
