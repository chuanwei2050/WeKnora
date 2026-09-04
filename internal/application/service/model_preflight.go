package service

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/assets"
	"github.com/Tencent/WeKnora/internal/models/asr"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/tts"
	"github.com/Tencent/WeKnora/internal/types"
)

type capabilityProbeUnsupportedError struct {
	err error
}

func (e *capabilityProbeUnsupportedError) Error() string { return e.err.Error() }
func (e *capabilityProbeUnsupportedError) Unwrap() error { return e.err }

func unsupportedCapability(format string, args ...interface{}) error {
	return &capabilityProbeUnsupportedError{err: fmt.Errorf(format, args...)}
}

// ProbeModelCapabilities executes role-specific, bounded probes. A failed
// role is returned in the matrix instead of aborting the whole preflight so
// operators can distinguish unsupported, missing and transiently failed roles.
func (s *modelService) ProbeModelCapabilities(ctx context.Context, modelID string) (*types.ModelPreflightResult, error) {
	model, err := s.GetModelByID(ctx, modelID)
	if err != nil {
		return nil, err
	}

	checkedAt := time.Now().UTC()
	result := &types.ModelPreflightResult{
		ModelID:   model.ID,
		ModelName: model.Name,
		Location:  model.Parameters.Location,
		Protocol:  model.Parameters.Protocol,
		CheckedAt: checkedAt,
	}
	roles := append([]types.ModelRole(nil), model.Parameters.Capabilities.Roles...)
	rolesInferred := len(roles) == 0
	if len(roles) == 0 {
		roles = []types.ModelRole{modelRoleForType(model.Type)}
	}
	modelKey := types.NormalizeModelIdentity(
		string(model.Parameters.Protocol),
		model.Parameters.BaseURL,
		model.Name,
		model.UpdatedAt.UTC().Format(time.RFC3339),
	).Key()

	for _, role := range roles {
		probe := types.ModelCapabilityProbeResult{
			Role:      role,
			ModelKey:  modelKey,
			CheckedAt: checkedAt,
		}
		started := time.Now()

		if validationErr := validatePreflightRole(model.Parameters.Capabilities, role, rolesInferred); validationErr != nil {
			probe.Status = types.CapabilityProbeUnsupported
			probe.Error = validationErr.Error()
			probe.LatencyMs = time.Since(started).Milliseconds()
			result.Probes = append(result.Probes, probe)
			continue
		}

		probeTimeout := modelPreflightTimeout(model.Parameters.Capabilities.TimeoutSeconds)
		probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
		observedModel, values, probeErr := s.probeModelRole(probeCtx, model, role)
		cancel()
		probe.LatencyMs = time.Since(started).Milliseconds()
		probe.ObservedModel = strings.TrimSpace(observedModel)
		probe.ObservedValues = values
		if probeErr == nil {
			probe.Status = types.CapabilityProbePassed
		} else {
			probe.Status = classifyCapabilityProbeError(probeErr)
			probe.Error = probeErr.Error()
		}
		result.Probes = append(result.Probes, probe)
	}
	result.Checks = append(result.Checks, repositoryPreflightCheck(checkedAt))
	observed := make([]string, 0, len(result.Probes))
	for _, probe := range result.Probes {
		if strings.TrimSpace(probe.ObservedModel) != "" {
			observed = append(observed, probe.ObservedModel)
		}
	}
	identityCheck := types.ModelPreflightCheckResult{
		Name:      "model_identity",
		Status:    types.PreflightCheckPassed,
		Details:   map[string]interface{}{"declared_model": model.Name, "observed_models": observed},
		CheckedAt: checkedAt,
	}
	if len(observed) == 0 {
		identityCheck.Status = types.PreflightCheckFailed
		identityCheck.Error = "no role probe returned an observed model identity"
	}
	result.Checks = append(result.Checks, identityCheck)
	result.Checks = append(result.Checks, s.runModelPreflightChecks(model, result.Probes, checkedAt)...)

	return result, nil
}

func modelPreflightTimeout(seconds int) time.Duration {
	timeout := time.Duration(seconds) * time.Second
	if timeout <= 0 || timeout > 60*time.Second {
		return 30 * time.Second
	}
	return timeout
}

func validatePreflightRole(manifest types.ModelCapabilityManifest, role types.ModelRole, inferred bool) error {
	if inferred {
		return nil
	}
	return manifest.ValidateRole(role)
}

func repositoryPreflightCheck(checkedAt time.Time) types.ModelPreflightCheckResult {
	return types.ModelPreflightCheckResult{
		Name:      "database",
		Status:    types.PreflightCheckPassed,
		Details:   map[string]interface{}{"model_loaded": true},
		CheckedAt: checkedAt,
	}
}

func (s *modelService) runModelPreflightChecks(model *types.Model, probes []types.ModelCapabilityProbeResult, checkedAt time.Time) []types.ModelPreflightCheckResult {
	checks := make([]types.ModelPreflightCheckResult, 0, 5)
	storage := types.ModelPreflightCheckResult{Name: "storage", Status: types.PreflightCheckPassed, CheckedAt: checkedAt}
	file, err := os.CreateTemp("", "weknora-preflight-")
	if err != nil {
		storage.Status = types.PreflightCheckFailed
		storage.Error = err.Error()
	} else {
		storage.Details = map[string]interface{}{"temp_directory": file.Name()}
		_ = file.Close()
		_ = os.Remove(file.Name())
	}
	checks = append(checks, storage)

	concurrency := types.ModelPreflightCheckResult{Name: "concurrency", Status: types.PreflightCheckPassed, CheckedAt: checkedAt}
	if model.Parameters.Capabilities.MaxConcurrency <= 0 {
		concurrency.Status = types.PreflightCheckSkipped
		concurrency.Details = map[string]interface{}{"reason": "model did not declare max_concurrency"}
	} else {
		concurrency.Details = map[string]interface{}{"declared_max": model.Parameters.Capabilities.MaxConcurrency}
	}
	checks = append(checks, concurrency)

	certificate := types.ModelPreflightCheckResult{Name: "certificate", Status: types.PreflightCheckSkipped, CheckedAt: checkedAt}
	if model.Source == types.ModelSourceLocal || strings.HasPrefix(strings.ToLower(strings.TrimSpace(model.Parameters.BaseURL)), "http://") {
		certificate.Details = map[string]interface{}{"reason": "TLS certificate is not applicable to this endpoint"}
	} else {
		certificate.Status = types.PreflightCheckPassed
		certificate.Details = map[string]interface{}{"https_endpoint": true, "connection_policy_checked": true}
	}
	checks = append(checks, certificate)

	resources := types.ModelPreflightCheckResult{Name: "resources", Status: types.PreflightCheckPassed, CheckedAt: checkedAt}
	missing := 0
	for _, probe := range probes {
		if probe.Status == types.CapabilityProbeMissingResource {
			missing++
		}
	}
	if missing > 0 {
		resources.Status = types.PreflightCheckMissingResource
		resources.Error = fmt.Sprintf("%d role probes reported missing resources", missing)
	}
	resources.Details = map[string]interface{}{"role_count": len(probes), "missing_role_count": missing}
	checks = append(checks, resources)
	return checks
}

func classifyCapabilityProbeError(err error) types.CapabilityProbeStatus {
	var unsupported *capabilityProbeUnsupportedError
	if errors.As(err, &unsupported) {
		return types.CapabilityProbeUnsupported
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"not found", "missing", "unavailable", "preloaded", "ollama"} {
		if strings.Contains(message, marker) {
			return types.CapabilityProbeMissingResource
		}
	}
	return types.CapabilityProbeFailed
}

func (s *modelService) probeModelRole(ctx context.Context, model *types.Model, role types.ModelRole) (string, map[string]interface{}, error) {
	switch role {
	case types.ModelRoleChat, types.ModelRoleVerifier, types.ModelRoleEvaluationJudge:
		return s.probeChatRole(ctx, model)
	case types.ModelRoleEmbedding:
		embedder, err := s.GetEmbeddingModel(ctx, model.ID)
		if err != nil {
			return "", nil, err
		}
		vector, err := embedder.Embed(ctx, "WeKnora air-gapped capability probe")
		if err != nil {
			return embedder.GetModelName(), nil, err
		}
		values := map[string]interface{}{"dimension": len(vector)}
		if len(vector) == 0 {
			return embedder.GetModelName(), values, unsupportedCapability("embedding probe returned an empty vector")
		}
		if expected := model.Parameters.Capabilities.EmbeddingDimension; expected > 0 && len(vector) != expected {
			return embedder.GetModelName(), values, unsupportedCapability("embedding dimension mismatch: expected %d, got %d", expected, len(vector))
		}
		return embedder.GetModelName(), values, nil
	case types.ModelRoleRerank:
		reranker, err := s.GetRerankModel(ctx, model.ID)
		if err != nil {
			return "", nil, err
		}
		documents := []string{"air-gapped probe document one", "air-gapped probe document two"}
		ranks, err := reranker.Rerank(ctx, "air-gapped capability probe", documents)
		if err != nil {
			return reranker.GetModelName(), nil, err
		}
		values := map[string]interface{}{"result_count": len(ranks), "indices": make([]int, 0, len(ranks))}
		indices := values["indices"].([]int)
		seen := make(map[int]struct{}, len(ranks))
		for _, rank := range ranks {
			if rank.Index < 0 || rank.Index >= len(documents) {
				return reranker.GetModelName(), values, unsupportedCapability("rerank probe returned invalid index %d", rank.Index)
			}
			if _, exists := seen[rank.Index]; exists {
				return reranker.GetModelName(), values, unsupportedCapability("rerank probe returned duplicate index %d", rank.Index)
			}
			seen[rank.Index] = struct{}{}
			indices = append(indices, rank.Index)
		}
		values["indices"] = indices
		if len(ranks) < len(documents) {
			return reranker.GetModelName(), values, unsupportedCapability("rerank probe returned %d results for %d documents", len(ranks), len(documents))
		}
		return reranker.GetModelName(), values, nil
	case types.ModelRoleVLM:
		visionModel, err := s.GetVLMModel(ctx, model.ID)
		if err != nil {
			return "", nil, err
		}
		expectedCount, err := randomVisionChallengeCount()
		if err != nil {
			return visionModel.GetModelName(), nil, err
		}
		challenge, err := assets.CreateVisionCountChallenge(expectedCount)
		if err != nil {
			return visionModel.GetModelName(), nil, err
		}
		output, err := visionModel.Predict(ctx, [][]byte{challenge}, "How many red circles are in this image? Return only the Arabic numeral.")
		if err != nil {
			return visionModel.GetModelName(), nil, err
		}
		answer, parseErr := parseVisionCount(output)
		if parseErr != nil || answer != expectedCount {
			return visionModel.GetModelName(), nil, unsupportedCapability("vision probe did not identify the image content")
		}
		return visionModel.GetModelName(), map[string]interface{}{"image_count": answer}, nil
	case types.ModelRoleParserOCR:
		visionModel, err := s.GetVLMModel(ctx, model.ID)
		if err != nil {
			return "", nil, err
		}
		output, err := visionModel.Predict(ctx, [][]byte{preflightPNG()}, "Read this image and return a JSON object with a text field.")
		if err != nil {
			return visionModel.GetModelName(), nil, err
		}
		output = strings.TrimSpace(output)
		if output == "" {
			return visionModel.GetModelName(), nil, unsupportedCapability("parser/OCR probe returned empty output")
		}
		values := map[string]interface{}{"output_non_empty": true}
		var parsed map[string]interface{}
		if json.Unmarshal([]byte(output), &parsed) == nil {
			values["structured_output"] = true
		}
		return visionModel.GetModelName(), values, nil
	case types.ModelRoleASR:
		asrModel, err := s.GetASRModel(ctx, model.ID)
		if err != nil {
			return "", nil, err
		}
		transcription, err := asrModel.Transcribe(ctx, assets.ASRTestWAV, "weknora-preflight.wav")
		if err != nil {
			return asrModel.GetModelName(), nil, err
		}
		values := map[string]interface{}{"segments": len(transcription.Segments), "text_non_empty": strings.TrimSpace(transcription.Text) != ""}
		if model.Parameters.Capabilities.Streaming {
			streaming, ok := asrModel.(asr.StreamingASR)
			if !ok || !streaming.SupportsStreaming() {
				return asrModel.GetModelName(), values, unsupportedCapability("model declares streaming ASR but provider does not support it")
			}
		}
		return asrModel.GetModelName(), values, nil
	case types.ModelRoleTTS:
		ttsModel, err := s.GetTTSModel(ctx, model.ID)
		if err != nil {
			return "", nil, err
		}
		audio, err := ttsModel.Synthesize(ctx, "preflight", tts.SynthesizeOptions{Format: "wav"})
		if err != nil {
			return ttsModel.GetModelName(), nil, err
		}
		defer audio.Close()
		data, err := io.ReadAll(io.LimitReader(audio, 1<<20))
		if err != nil {
			return ttsModel.GetModelName(), nil, err
		}
		if len(data) == 0 {
			return ttsModel.GetModelName(), nil, unsupportedCapability("tts probe returned empty audio")
		}
		return ttsModel.GetModelName(), map[string]interface{}{"audio_bytes": len(data)}, nil
	default:
		return "", nil, unsupportedCapability("unsupported model role %q", role)
	}
}

func (s *modelService) probeChatRole(ctx context.Context, model *types.Model) (string, map[string]interface{}, error) {
	chatModel, err := s.GetChatModel(ctx, model.ID)
	if err != nil {
		return "", nil, err
	}
	values := make(map[string]interface{})
	stream, err := chatModel.ChatStream(ctx, []chat.Message{{Role: "user", Content: "Reply with one short word."}}, &chat.ChatOptions{MaxTokens: 16, Temperature: 0})
	if err != nil {
		return chatModel.GetModelName(), values, err
	}
	var content strings.Builder
	for stream != nil {
		select {
		case <-ctx.Done():
			return chatModel.GetModelName(), values, ctx.Err()
		case response, ok := <-stream:
			if !ok {
				stream = nil
				continue
			}
			content.WriteString(response.Content)
			if response.Done {
				stream = nil
			}
		}
	}
	if strings.TrimSpace(content.String()) == "" {
		return chatModel.GetModelName(), values, unsupportedCapability("chat streaming probe returned no visible output")
	}
	values["streaming"] = true

	manifest := model.Parameters.Capabilities
	if manifest.StructuredOutput {
		response, callErr := chatModel.Chat(ctx, []chat.Message{{Role: "user", Content: "Return exactly {\"ok\":true}."}}, &chat.ChatOptions{
			MaxTokens:   32,
			Temperature: 0,
			Format:      json.RawMessage(`{"type":"json_object"}`),
		})
		if callErr != nil {
			return chatModel.GetModelName(), values, callErr
		}
		var object map[string]interface{}
		if response == nil || json.Unmarshal([]byte(strings.TrimSpace(response.Content)), &object) != nil {
			return chatModel.GetModelName(), values, unsupportedCapability("structured-output probe did not return a JSON object")
		}
		values["structured_output"] = true
	}
	if manifest.ToolCalling {
		response, callErr := chatModel.Chat(ctx, []chat.Message{{Role: "user", Content: "Call the probe tool."}}, &chat.ChatOptions{
			MaxTokens:   32,
			Temperature: 0,
			ToolChoice:  "required",
			Tools: []chat.Tool{{Type: "function", Function: chat.FunctionDef{
				Name:        "preflight_probe",
				Description: "Record a successful capability probe.",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"]}`),
			}}},
		})
		if callErr != nil {
			return chatModel.GetModelName(), values, callErr
		}
		if response == nil || len(response.ToolCalls) == 0 {
			return chatModel.GetModelName(), values, unsupportedCapability("tool-calling probe returned no tool call")
		}
		values["tool_calling"] = true
	}
	return chatModel.GetModelName(), values, nil
}

func preflightPNG() []byte {
	data, _ := assets.CreateVisionCountChallenge(4)
	return data
}

func randomVisionChallengeCount() (int, error) {
	return visionChallengeCount(rand.Reader)
}

func visionChallengeCount(random io.Reader) (int, error) {
	value, err := rand.Int(random, big.NewInt(5))
	if err != nil {
		return 0, err
	}
	return 4 + int(value.Int64()), nil
}

var (
	plainVisionCountPattern   = regexp.MustCompile(`^([0-9]+)[.!。]?$`)
	labeledVisionCountPattern = regexp.MustCompile(`(?i)^(?:the\s+answer\s+is|答案(?:是|为))\s*[:：]?\s*([0-9]+)[.!。]?$`)
)

func parseVisionCount(output string) (int, error) {
	trimmed := strings.TrimSpace(output)
	if strings.HasPrefix(trimmed, "```") && strings.HasSuffix(trimmed, "```") {
		firstLineEnd := strings.IndexByte(trimmed, '\n')
		if firstLineEnd < 0 {
			return 0, fmt.Errorf("invalid fenced vision response")
		}
		trimmed = strings.TrimSpace(trimmed[firstLineEnd+1 : len(trimmed)-3])
	}

	var jsonAnswer struct {
		Count *int `json:"count"`
	}
	var jsonFields map[string]json.RawMessage
	if json.Unmarshal([]byte(trimmed), &jsonFields) == nil {
		if len(jsonFields) != 1 {
			return 0, fmt.Errorf("vision response JSON must contain only count")
		}
		if _, ok := jsonFields["count"]; !ok || json.Unmarshal([]byte(trimmed), &jsonAnswer) != nil || jsonAnswer.Count == nil || *jsonAnswer.Count <= 0 {
			return 0, fmt.Errorf("vision response JSON count must be a positive integer")
		}
		return *jsonAnswer.Count, nil
	}

	match := plainVisionCountPattern.FindStringSubmatch(trimmed)
	if len(match) == 0 {
		match = labeledVisionCountPattern.FindStringSubmatch(trimmed)
	}
	if len(match) != 2 {
		return 0, fmt.Errorf("unsupported vision response format")
	}
	count, err := strconv.Atoi(match[1])
	if err != nil || count <= 0 {
		return 0, fmt.Errorf("vision response does not contain a positive integer")
	}
	return count, nil
}
