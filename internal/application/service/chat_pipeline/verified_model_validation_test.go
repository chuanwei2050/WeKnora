package chatpipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	modelchat "github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type validationChatStub struct {
	content string
	opts    *modelchat.ChatOptions
	prompt  string
	calls   int
}

func (s *validationChatStub) Chat(_ context.Context, messages []modelchat.Message, opts *modelchat.ChatOptions) (*types.ChatResponse, error) {
	s.calls++
	s.opts = opts
	if len(messages) > 1 {
		s.prompt = messages[1].Content
	}
	return &types.ChatResponse{Content: s.content}, nil
}

func (s *validationChatStub) ChatStream(context.Context, []modelchat.Message, *modelchat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, nil
}

func (s *validationChatStub) GetModelName() string { return "validator" }
func (s *validationChatStub) GetModelID() string   { return "validator-id" }

type validationModelServiceStub struct {
	interfaces.ModelService
	model            *types.Model
	chat             modelchat.Chat
	specializedModel *types.Model
	specializedChat  modelchat.Chat
}

func (s *validationModelServiceStub) GetModelByID(context.Context, string) (*types.Model, error) {
	return s.model, nil
}

func (s *validationModelServiceStub) GetChatModel(_ context.Context, id string) (modelchat.Chat, error) {
	if s.specializedModel != nil && id == s.specializedModel.ID {
		return s.specializedChat, nil
	}
	return s.chat, nil
}

func (s *validationModelServiceStub) GetDefaultModel(_ context.Context, _ types.ModelType, role string) (*types.Model, error) {
	if s.specializedModel != nil && s.specializedModel.ProfileRole == role {
		return s.specializedModel, nil
	}
	return nil, errors.New("specialized model is not configured")
}

func TestGetAuxiliaryChatModelUsesPersistedRoleAndFallsBack(t *testing.T) {
	fallback := &validationChatStub{}
	specialized := &validationChatStub{}
	service := &validationModelServiceStub{
		chat:             fallback,
		specializedModel: &types.Model{ID: "query-model", ProfileRole: types.ModelProfileRoleQueryUnderstand},
		specializedChat:  specialized,
	}

	got, selected, err := getAuxiliaryChatModel(context.Background(), service, types.ModelProfileRoleQueryUnderstand, "main-model")
	if err != nil || !selected || got != specialized {
		t.Fatalf("specialized selection = %#v, %v, %v", got, selected, err)
	}
	got, selected, err = getAuxiliaryChatModel(context.Background(), service, "unconfigured-role", "main-model")
	if err != nil || selected || got != fallback {
		t.Fatalf("fallback selection = %#v, %v, %v", got, selected, err)
	}
}

func TestValidateWithChatModelParsesStrictStructuredOutput(t *testing.T) {
	stub := &validationChatStub{content: `{"fact_score":0.9,"logic_score":0.8,"citation_score":0.7,"completeness_score":0.6,"issues":[{"claim_id":"claim-1","evidence_ids":["evidence-1"],"dimension":"citation","severity":"warning","message":"citation is incomplete"}]}`}
	identity := types.NormalizeModelIdentity("openai-compatible", "https://validator.example/v1", "validator", "1")
	report, err := validateWithChatModel(context.Background(), stub, identity, types.DraftAnswer{Text: "answer", Claims: []types.Claim{{ID: "claim-1", Text: "answer", EvidenceIDs: []string{"evidence-1"}}}}, types.EvidenceBundle{Items: []types.Evidence{{ID: "evidence-1", Content: "source"}}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Model.Key() != identity.Key() || report.FactScore != .9 || len(report.Issues) != 1 {
		t.Fatalf("unexpected validation report: %#v", report)
	}
	if stub.opts == nil || len(stub.opts.Format) == 0 || !strings.Contains(stub.prompt, "evidence-1") {
		t.Fatalf("structured validation request was not built: opts=%#v prompt=%q", stub.opts, stub.prompt)
	}
}

func TestParseModelValidationOutputRejectsProseAndUnknownFields(t *testing.T) {
	identity := types.NormalizeModelIdentity("openai-compatible", "https://validator.example", "validator", "1")
	valid := `{"fact_score":1,"logic_score":1,"citation_score":1,"completeness_score":1}`
	for name, raw := range map[string]string{
		"markdown":      "```json\n" + valid + "\n```",
		"unknown field": `{"fact_score":1,"logic_score":1,"citation_score":1,"completeness_score":1,"thinking":"hidden"}`,
		"missing score": `{"fact_score":1,"logic_score":1,"citation_score":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseModelValidationOutput(raw, identity); err == nil {
				t.Fatal("expected strict validation output rejection")
			}
		})
	}
}

func TestPluginUsesConfiguredChatModelForValidation(t *testing.T) {
	stub := &validationChatStub{content: `{"fact_score":1,"logic_score":1,"citation_score":1,"completeness_score":1}`}
	modelService := &validationModelServiceStub{
		model: &types.Model{ID: "validator-id", Name: "validator", Parameters: types.ModelParameters{Protocol: "openai-compatible", BaseURL: "https://validator.example/v1"}},
		chat:  stub,
	}
	plugin := &PluginVerifiedAnswer{modelService: modelService}
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			TenantID:    1,
			SessionID:   "session-1",
			ChatModelID: "validator-id",
			VerifiedAnswer: types.VerifiedAnswerConfig{
				Enabled: true,
				Weights: types.ValidationWeights{Fact: 1, Logic: 1, Citation: 1, Completeness: 1, PassThreshold: .8, ReflectThreshold: .5},
			},
		},
		PipelineState: types.PipelineState{ChatResponse: &types.ChatResponse{Content: "draft"}},
	}
	answer, err := plugin.execute(context.Background(), manage)
	if err != nil || answer == nil || answer.Decision != types.VerificationPassed || stub.calls != 1 {
		t.Fatalf("configured model was not used: answer=%+v err=%v calls=%d", answer, err, stub.calls)
	}
}
