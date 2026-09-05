package chatpipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/event"
	modelchat "github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
)

// PluginVerifiedAnswer runs after ordinary completion. It deliberately wraps
// the existing answer instead of changing the default-off pipeline contract.
type PluginVerifiedAnswer struct {
	modelService interfaces.ModelService
}

const verificationValidatorMaxTokens = 2048

func NewPluginVerifiedAnswer(eventManager *EventManager, modelService interfaces.ModelService) *PluginVerifiedAnswer {
	plugin := &PluginVerifiedAnswer{modelService: modelService}
	eventManager.Register(plugin)
	return plugin
}

func (p *PluginVerifiedAnswer) ActivationEvents() []types.EventType {
	return []types.EventType{types.CHAT_COMPLETION}
}

func (p *PluginVerifiedAnswer) OnEvent(ctx context.Context, _ types.EventType, chatManage *types.ChatManage, next func() *PluginError) *PluginError {
	if chatManage == nil || !chatManage.VerifiedAnswer.Enabled || chatManage.ChatResponse == nil {
		return next()
	}
	if _, err := p.execute(ctx, chatManage); err != nil {
		chatManage.ChatResponse.Content = "验证未完成，暂无法确认该回答。"
		return next()
	}
	return next()
}

// execute runs verification without registering a pipeline plugin. Streaming
// callers use the same path after buffering the model draft internally.
func (p *PluginVerifiedAnswer) execute(ctx context.Context, chatManage *types.ChatManage) (*types.VerifiedAnswer, error) {
	start := time.Now()
	emitVerificationStage(ctx, chatManage, types.VerificationStageEvent{Stage: "verification", Status: "started"})
	bundle := evidenceBundleFromResults(chatManage)
	identities := p.modelIdentities(ctx, chatManage)
	verificationModels, err := p.loadVerificationModels(ctx, chatManage)
	if err != nil {
		return nil, err
	}
	currentDraftText := chatManage.ChatResponse.Content
	initialUsage := chatManage.ChatResponse.Usage
	if initialUsage.PromptTokens == 0 && initialUsage.CompletionTokens == 0 {
		initialUsage.PromptTokens = estimateVerificationTokens(chatManage.Query, chatManage.RenderedContexts, chatManage.UserContent)
		initialUsage.CompletionTokens = estimateVerificationTokens(currentDraftText)
		initialUsage.TotalTokens = initialUsage.PromptTokens + initialUsage.CompletionTokens
	}
	coordinator := types.NewVerifiedAnswerCoordinator(chatManage.VerifiedAnswer)
	scope := verificationScope(chatManage)
	regenerateAfterRetrieval := false
	answer, err := coordinator.Execute(ctx, chatManage.Query, identities, types.VerificationHooks{
		Scope:           &scope,
		RoutingDecision: chatManage.RoutingDecision,
		InitialUsage:    initialUsage,
		EstimateValidationBudget: func(draft types.DraftAnswer, evidence types.EvidenceBundle) types.VerificationBudgetEstimate {
			if len(verificationModels) == 0 {
				return types.VerificationBudgetEstimate{}
			}
			return types.VerificationBudgetEstimate{ModelCalls: len(verificationModels), InputTokens: verificationInputTokenEstimate(draft, evidence), OutputTokens: verificationValidatorMaxTokens}
		},
		EstimateReflectionBudget: func(draft types.DraftAnswer, evidence types.EvidenceBundle, reports []types.ValidationReport) types.VerificationBudgetEstimate {
			modelCalls := 0
			if len(verificationModels) > 0 {
				modelCalls = 1
			}
			return types.VerificationBudgetEstimate{ModelCalls: modelCalls, InputTokens: verificationReflectionInputTokenEstimate(draft.Text, evidence, reports), OutputTokens: 1024}
		},
		EstimateRetrievalBudget: func(types.RetrievalRequest) types.VerificationBudgetEstimate {
			return types.VerificationBudgetEstimate{ModelCalls: 1, InputTokens: estimateVerificationTokens(chatManage.Query), OutputTokens: 0}
		},
		Retrieve: func(retrieveCtx context.Context, retrieveQuery string) (types.EvidenceBundle, error) {
			if len(bundle.Items) > 0 || chatManage.VerifiedRetrieve == nil {
				return bundle, nil
			}
			results, retrieveErr := chatManage.VerifiedRetrieve(retrieveCtx, retrieveQuery)
			if retrieveErr != nil {
				return types.EvidenceBundle{}, retrieveErr
			}
			bundle = evidenceBundleFromResultsForScope(chatManage, results, scope)
			return bundle, nil
		},
		RetrieveMore: func(retrieveCtx context.Context, request types.RetrievalRequest) (types.EvidenceBundle, error) {
			if request.Scope.Key() != scope.Key() {
				return types.EvidenceBundle{}, fmt.Errorf("reflection scope changed")
			}
			if chatManage.VerifiedRetrieve == nil {
				return types.EvidenceBundle{}, nil
			}
			results, retrieveErr := chatManage.VerifiedRetrieve(retrieveCtx, request.Query)
			if retrieveErr != nil {
				return types.EvidenceBundle{}, retrieveErr
			}
			regenerateAfterRetrieval = true
			bundle = evidenceBundleFromResultsForScope(chatManage, results, request.Scope)
			return bundle, nil
		},
		Draft: func(draftCtx context.Context, _ string, draftEvidence types.EvidenceBundle) (types.DraftAnswer, error) {
			if regenerateAfterRetrieval {
				if len(verificationModels) == 0 {
					return types.DraftAnswer{}, fmt.Errorf("retrieval regeneration model is unavailable")
				}
				rewritten, rewriteErr := rewriteDraftWithChatModel(draftCtx, verificationModels[0].model, draftFromResponse(currentDraftText, bundle), draftEvidence, nil)
				if rewriteErr != nil {
					return types.DraftAnswer{}, rewriteErr
				}
				currentDraftText = rewritten
				regenerateAfterRetrieval = false
			}
			return draftFromResponse(currentDraftText, draftEvidence), nil
		},
		Validate: func(_ context.Context, draft types.DraftAnswer, evidence types.EvidenceBundle) (types.ValidationReport, error) {
			return deterministicValidationReport(identities, draft, evidence), nil
		},
		ValidateMany: func(ctx context.Context, draft types.DraftAnswer, evidence types.EvidenceBundle) ([]types.ValidationReport, error) {
			if len(verificationModels) == 0 {
				return deterministicValidationReports(identities, draft, evidence), nil
			}
			reports := make([]types.ValidationReport, len(verificationModels))
			var wg sync.WaitGroup
			var firstErr error
			var errMu sync.Mutex
			for index, verifier := range verificationModels {
				wg.Add(1)
				go func(index int, verifier verificationModel) {
					defer wg.Done()
					if err := ctx.Err(); err != nil {
						errMu.Lock()
						if firstErr == nil {
							firstErr = err
						}
						errMu.Unlock()
						reports[index] = degradedValidationReport(verifier.identity, err.Error())
						return
					}
					report, validationErr := validateWithChatModel(ctx, verifier.model, verifier.identity, draft, evidence)
					if validationErr != nil {
						// Keep a degraded structured report so aggregation can
						// still enter reflection instead of aborting the round.
						reports[index] = degradedValidationReport(verifier.identity, validationErr.Error())
						return
					}
					reports[index] = report
				}(index, verifier)
			}
			wg.Wait()
			if firstErr != nil && ctx.Err() != nil {
				return reports, firstErr
			}
			return reports, nil
		},
		Reflect: func(reflectCtx context.Context, draft types.DraftAnswer, reports []types.ValidationReport) (types.ReflectionPlan, error) {
			if len(verificationModels) == 0 {
				return types.ReflectionPlan{Action: types.ReflectionStop, Reason: "pipeline validator has no independent rewrite provider"}, nil
			}
			if chatManage.VerifiedRetrieve != nil && len(bundle.Items) == 0 {
				return types.ReflectionPlan{Action: types.ReflectionRetrieveMore, Reason: "validator requested additional scoped evidence"}, nil
			}
			rewritten, rewriteErr := rewriteDraftWithChatModel(reflectCtx, verificationModels[0].model, draft, bundle, reports)
			if rewriteErr != nil {
				return types.ReflectionPlan{}, rewriteErr
			}
			currentDraftText = rewritten
			return types.ReflectionPlan{Action: types.ReflectionRewrite}, nil
		},
	})
	if err != nil {
		emitVerificationStage(ctx, chatManage, types.VerificationStageEvent{Stage: "verification", Status: "degraded", Degraded: true, Reason: err.Error(), DurationMillis: time.Since(start).Milliseconds()})
		pipelineWarn(ctx, "VerifiedAnswer", "disabled_or_failed", map[string]interface{}{"session_id": chatManage.SessionID, "error": err.Error()})
		return nil, err
	}
	chatManage.VerifiedResult = answer
	if visible := verifiedVisibleText(answer); strings.TrimSpace(visible) != "" {
		chatManage.ChatResponse.Content = visible
	}
	emitVerificationStage(ctx, chatManage, types.VerificationStageEvent{
		Stage: "verification", Status: "completed", Decision: answer.Decision, Confidence: answer.Confidence,
		Degraded: answer.Degraded, DurationMillis: time.Since(start).Milliseconds(), ModelKeys: reportModelKeys(answer.Reports),
	})
	pipelineInfo(ctx, "VerifiedAnswer", "completed", map[string]interface{}{
		"session_id":  chatManage.SessionID,
		"decision":    answer.Decision,
		"confidence":  answer.Confidence,
		"degraded":    answer.Degraded,
		"duration_ms": time.Since(start).Milliseconds(),
	})
	return answer, nil
}

// VerifyAnswer runs the same coordinator used by the normal RAG completion
// plugin for a buffered answer from another execution path, such as AgentQA.
func VerifyAnswer(ctx context.Context, modelService interfaces.ModelService, chatManage *types.ChatManage) (*types.VerifiedAnswer, error) {
	return (&PluginVerifiedAnswer{modelService: modelService}).execute(ctx, chatManage)
}

// VisibleVerifiedText returns the only answer text that may be exposed after
// verification, including the conservative note when verification degraded.
func VisibleVerifiedText(answer *types.VerifiedAnswer) string {
	return verifiedVisibleText(answer)
}

func verifiedVisibleText(answer *types.VerifiedAnswer) string {
	if answer == nil {
		return ""
	}
	if !answer.Degraded {
		return answer.Text
	}
	note := strings.TrimSpace(answer.ConservativeNote)
	text := strings.TrimSpace(answer.Text)
	switch {
	case note != "" && text != "":
		return note + "\n\n" + text
	case note != "":
		return note
	default:
		return answer.Text
	}
}

func verificationInputTokenEstimate(draft types.DraftAnswer, evidence types.EvidenceBundle) int {
	return estimateVerificationTokens("You are an independent answer validator.", buildValidationPrompt(draft, evidence))
}

func verificationReflectionInputTokenEstimate(draft string, evidence types.EvidenceBundle, reports []types.ValidationReport) int {
	reportPayload, _ := json.Marshal(reports)
	return estimateVerificationTokens("You rewrite an answer after independent validation found issues.", buildValidationPrompt(types.DraftAnswer{Text: draft}, evidence), string(reportPayload))
}

func estimateVerificationTokens(parts ...string) int {
	chars := 0
	for _, part := range parts {
		// Reserve by UTF-8 byte length. It is intentionally an upper bound so
		// the budget cannot be bypassed when a provider omits usage metadata or
		// when non-ASCII evidence produces more tokens than a rune estimate.
		chars += len([]byte(part))
	}
	if chars == 0 {
		return 1
	}
	return chars
}

func evidenceBundleFromResults(chatManage *types.ChatManage) types.EvidenceBundle {
	results := chatManage.MergeResult
	if len(results) == 0 {
		results = chatManage.SearchResult
	}
	bundle := types.EvidenceBundle{ID: "pipeline-evidence", Query: chatManage.Query, ScopeKey: verificationScope(chatManage).Key()}
	for _, result := range results {
		if result == nil || result.ID == "" {
			continue
		}
		bundle.Items = append(bundle.Items, types.Evidence{ID: result.ID, Content: result.Content, Source: result.KnowledgeTitle, KnowledgeID: result.KnowledgeID, KnowledgeBaseID: result.KnowledgeBaseID, KnowledgeVersionID: result.KnowledgeVersionID, ChunkID: result.ID})
	}
	if chatManage.GraphSearchResult != nil {
		seen := make(map[string]bool, len(bundle.Items))
		for _, item := range bundle.Items {
			seen[item.ID] = true
		}
		for _, citation := range chatManage.GraphSearchResult.Citations {
			if citation.ChunkID == "" || seen[citation.ChunkID] {
				continue
			}
			seen[citation.ChunkID] = true
			bundle.Items = append(bundle.Items, types.Evidence{ID: citation.ChunkID, Source: citation.Source, KnowledgeID: citation.KnowledgeID, KnowledgeVersionID: citation.KnowledgeVersionID, ChunkID: citation.ChunkID})
		}
	}
	return bundle
}

func evidenceBundleFromResultsForScope(chatManage *types.ChatManage, results []*types.SearchResult, scope types.VerificationScope) types.EvidenceBundle {
	bundle := types.EvidenceBundle{ID: "reflection-evidence", Query: chatManage.Query, ScopeKey: scope.Key()}
	for _, result := range results {
		if result == nil || result.ID == "" {
			continue
		}
		bundle.Items = append(bundle.Items, types.Evidence{
			ID: result.ID, Content: result.Content, Source: result.KnowledgeTitle,
			KnowledgeID: result.KnowledgeID, KnowledgeBaseID: result.KnowledgeBaseID,
			KnowledgeVersionID: result.KnowledgeVersionID, ChunkID: result.ID,
		})
	}
	return bundle
}

func verificationScope(chatManage *types.ChatManage) types.VerificationScope {
	if chatManage == nil {
		return types.VerificationScope{}
	}
	versionID := ""
	mixedVersions := false
	setVersion := func(candidate string) {
		if mixedVersions {
			return
		}
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return
		}
		if versionID == "" {
			versionID = candidate
			return
		}
		if versionID != candidate {
			versionID = ""
			mixedVersions = true
		}
	}
	results := chatManage.MergeResult
	if len(results) == 0 {
		results = chatManage.SearchResult
	}
	for _, result := range results {
		if result != nil {
			setVersion(result.KnowledgeVersionID)
		}
	}
	if chatManage.GraphSearchResult != nil {
		for _, citation := range chatManage.GraphSearchResult.Citations {
			setVersion(citation.KnowledgeVersionID)
		}
	}
	return types.VerificationScope{
		TenantID: chatManage.TenantID, SessionID: chatManage.SessionID,
		KnowledgeBaseIDs:   append([]string(nil), chatManage.KnowledgeBaseIDs...),
		KnowledgeIDs:       append([]string(nil), chatManage.KnowledgeIDs...),
		KnowledgeVersionID: versionID,
	}
}

func draftFromResponse(text string, bundle types.EvidenceBundle) types.DraftAnswer {
	text = unwrapAgentDraftText(text)
	draft := types.DraftAnswer{ID: "pipeline-draft", Text: text}
	if len(bundle.Items) > 0 {
		ids := make([]string, 0, len(bundle.Items))
		for _, evidence := range bundle.Items {
			ids = append(ids, evidence.ID)
		}
		draft.Claims = []types.Claim{{ID: "answer-claim", Text: text, EvidenceIDs: ids, Core: true}}
	}
	return draft
}

// unwrapAgentDraftText removes the structured envelope emitted by the
// final_answer tool while preserving ordinary JSON answers unchanged.
func unwrapAgentDraftText(text string) string {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "```") && strings.HasSuffix(trimmed, "```") {
		trimmed = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "```json"), "```"))
	}
	var envelope struct {
		Draft *struct {
			Text string `json:"text"`
		} `json:"draft"`
	}
	if err := json.Unmarshal([]byte(trimmed), &envelope); err == nil && envelope.Draft != nil && strings.TrimSpace(envelope.Draft.Text) != "" {
		return strings.TrimSpace(envelope.Draft.Text)
	}
	return text
}

func degradedValidationReport(identity types.ModelIdentity, reason string) types.ValidationReport {
	return types.ValidationReport{
		ID:                uuid.NewString(),
		Model:             identity,
		FactScore:         0.4,
		LogicScore:        0.4,
		CitationScore:     0.4,
		CompletenessScore: 0.4,
		Degraded:          true,
		DegradationReason: truncateValidationText(reason, 400),
	}
}

func deterministicValidationReport(identities []types.ModelIdentity, draft types.DraftAnswer, bundle types.EvidenceBundle) types.ValidationReport {
	model := types.ModelIdentity{ProtocolProvider: "pipeline", BaseEndpoint: "local", ModelName: "deterministic-validator", Version: "1"}
	if len(identities) > 0 && identities[0].Key() != "|||" {
		model = identities[0]
	}
	return deterministicValidationReportForModel(model, draft, bundle)
}

func deterministicValidationReports(identities []types.ModelIdentity, draft types.DraftAnswer, bundle types.EvidenceBundle) []types.ValidationReport {
	if len(identities) == 0 {
		return []types.ValidationReport{deterministicValidationReport(nil, draft, bundle)}
	}
	reports := make([]types.ValidationReport, 0, len(identities))
	for _, identity := range identities {
		reports = append(reports, deterministicValidationReportForModel(identity, draft, bundle))
	}
	return reports
}

func deterministicValidationReportForModel(model types.ModelIdentity, draft types.DraftAnswer, bundle types.EvidenceBundle) types.ValidationReport {
	return types.ValidateDraftAnswer(draft, bundle, model)
}

type verificationModel struct {
	identity types.ModelIdentity
	model    modelchat.Chat
}

func (p *PluginVerifiedAnswer) loadVerificationModels(ctx context.Context, chatManage *types.ChatManage) ([]verificationModel, error) {
	ids := verificationModelIDs(chatManage)
	if len(ids) == 0 {
		return nil, nil
	}
	if p == nil || p.modelService == nil {
		return nil, fmt.Errorf("verification models are configured but model service is unavailable")
	}
	models := make([]verificationModel, 0, len(ids))
	seenIdentities := map[string]bool{}
	for _, id := range ids {
		modelRecord, err := p.modelService.GetModelByID(ctx, id)
		if err != nil || modelRecord == nil {
			if err == nil {
				err = fmt.Errorf("model %q not found", id)
			}
			return nil, err
		}
		model, err := p.modelService.GetChatModel(ctx, id)
		if err != nil {
			return nil, err
		}
		identity := modelIdentityFromRecord(modelRecord)
		if seenIdentities[identity.Key()] {
			continue
		}
		seenIdentities[identity.Key()] = true
		models = append(models, verificationModel{identity: identity, model: model})
	}
	return models, nil
}

func verificationModelIDs(chatManage *types.ChatManage) []string {
	if chatManage == nil {
		return nil
	}
	ids := []string{chatManage.ChatModelID, chatManage.VerifiedAnswer.FactValidatorModelID, chatManage.VerifiedAnswer.LogicValidatorModelID, chatManage.VerifiedAnswer.CitationValidatorModelID}
	seen := map[string]bool{}
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, id)
	}
	return result
}

func modelIdentityFromRecord(model *types.Model) types.ModelIdentity {
	protocol := string(model.Parameters.Protocol)
	if strings.TrimSpace(protocol) == "" {
		protocol = model.Parameters.Provider
	}
	return types.NormalizeModelIdentity(protocol, model.Parameters.BaseURL, model.Name, model.UpdatedAt.UTC().Format(time.RFC3339))
}

func validateWithChatModel(ctx context.Context, model modelchat.Chat, identity types.ModelIdentity, draft types.DraftAnswer, evidence types.EvidenceBundle) (types.ValidationReport, error) {
	if model == nil {
		return types.ValidationReport{}, fmt.Errorf("validator model is nil")
	}
	disableThinking := false
	response, err := model.Chat(ctx, []modelchat.Message{
		{Role: "system", Content: "You are an independent answer validator. Evaluate the draft against the supplied evidence. Return exactly one JSON object and no markdown with keys fact_score, logic_score, citation_score, completeness_score (each 0..1), and issues (array). If there are no issues, return \"issues\":[]. Each issue must use an existing draft claim_id (usually \"answer-claim\"), existing evidence_ids from the input, dimension in {fact,logic,citation,completeness}, severity in {info,warning,critical}, and a short message. Do not invent claim or evidence IDs. Do not return chain-of-thought, hidden reasoning, markdown fences, or prose."},
		{Role: "user", Content: buildValidationPrompt(draft, evidence)},
	}, &modelchat.ChatOptions{
		Temperature: 0,
		MaxTokens:   verificationValidatorMaxTokens,
		Thinking:    &disableThinking,
		Format:      json.RawMessage(`{"type":"object"}`),
	})
	if err != nil {
		return types.ValidationReport{}, err
	}
	if response == nil || strings.TrimSpace(response.Content) == "" {
		return types.ValidationReport{}, fmt.Errorf("validator returned empty output")
	}
	report, err := parseModelValidationOutput(response.Content, identity)
	if err != nil {
		return types.ValidationReport{}, err
	}
	claimIDs := make(map[string]bool, len(draft.Claims))
	for _, claim := range draft.Claims {
		claimIDs[claim.ID] = true
	}
	evidenceIDs := make(map[string]bool, len(evidence.Items))
	for _, item := range evidence.Items {
		evidenceIDs[item.ID] = true
	}
	for _, issue := range report.Issues {
		if strings.TrimSpace(issue.ClaimID) == "" || len(issue.EvidenceIDs) == 0 {
			return types.ValidationReport{}, fmt.Errorf("validator issue must bind a claim and evidence")
		}
		if !claimIDs[issue.ClaimID] {
			return types.ValidationReport{}, fmt.Errorf("validator issue references unknown claim %q", issue.ClaimID)
		}
		for _, evidenceID := range issue.EvidenceIDs {
			if !evidenceIDs[evidenceID] {
				return types.ValidationReport{}, fmt.Errorf("validator issue references unknown evidence %q", evidenceID)
			}
		}
	}
	return report, nil
}

func rewriteDraftWithChatModel(ctx context.Context, model modelchat.Chat, draft types.DraftAnswer, evidence types.EvidenceBundle, reports []types.ValidationReport) (string, error) {
	if model == nil {
		return "", fmt.Errorf("rewrite model is nil")
	}
	reportPayload, err := json.Marshal(reports)
	if err != nil {
		return "", fmt.Errorf("marshal validation reports: %w", err)
	}
	response, err := model.Chat(ctx, []modelchat.Message{
		{Role: "system", Content: "You rewrite an answer after independent validation found issues. Use only the supplied evidence, fix unsupported or incomplete claims, and return only the revised answer. Do not expose physical table names, internal identifiers, internally generated aliases, or raw result payloads. Convert needed structured results into natural language. If the user explicitly asks about SQL, table structure, or business column names, answer that request; otherwise do not expose SQL or the query process. Do not return analysis, validator commentary, chain-of-thought, or a confidence explanation."},
		{Role: "user", Content: buildValidationPrompt(draft, evidence) + "\nValidation issues to address:\n" + string(reportPayload)},
	}, &modelchat.ChatOptions{Temperature: 0, MaxTokens: 1024})
	if err != nil {
		return "", err
	}
	if response == nil || strings.TrimSpace(response.Content) == "" {
		return "", fmt.Errorf("rewrite model returned empty output")
	}
	return strings.TrimSpace(response.Content), nil
}

func buildValidationPrompt(draft types.DraftAnswer, evidence types.EvidenceBundle) string {
	boundedDraft := draft
	boundedDraft.Text = truncateValidationText(draft.Text, 8000)
	boundedDraft.Claims = make([]types.Claim, len(draft.Claims))
	copy(boundedDraft.Claims, draft.Claims)
	for index := range boundedDraft.Claims {
		boundedDraft.Claims[index].Text = truncateValidationText(boundedDraft.Claims[index].Text, 2000)
	}
	boundedEvidence := evidence
	boundedEvidence.Items = make([]types.Evidence, 0, len(evidence.Items))
	for _, item := range evidence.Items {
		item.Content = truncateValidationText(item.Content, 4000)
		boundedEvidence.Items = append(boundedEvidence.Items, item)
	}
	payload, err := json.Marshal(struct {
		Draft    types.DraftAnswer    `json:"draft"`
		Evidence types.EvidenceBundle `json:"evidence"`
	}{Draft: boundedDraft, Evidence: boundedEvidence})
	if err != nil {
		return "Unable to serialize validation input."
	}
	return "Validate this JSON input:\n" + string(payload)
}

func parseModelValidationOutput(raw string, identity types.ModelIdentity) (types.ValidationReport, error) {
	var envelope struct {
		FactScore         *float64                `json:"fact_score"`
		LogicScore        *float64                `json:"logic_score"`
		CitationScore     *float64                `json:"citation_score"`
		CompletenessScore *float64                `json:"completeness_score"`
		Issues            []types.ValidationIssue `json:"issues,omitempty"`
	}
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return types.ValidationReport{}, fmt.Errorf("parse validator JSON: %w", err)
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("trailing JSON value")
		}
		return types.ValidationReport{}, fmt.Errorf("parse validator JSON: %w", err)
	}
	if envelope.FactScore == nil || envelope.LogicScore == nil || envelope.CitationScore == nil || envelope.CompletenessScore == nil {
		return types.ValidationReport{}, fmt.Errorf("validator scores are required")
	}
	for index, issue := range envelope.Issues {
		switch issue.Dimension {
		case types.ValidationFact, types.ValidationLogic, types.ValidationCitation, types.ValidationCompleteness:
		default:
			return types.ValidationReport{}, fmt.Errorf("unknown validation dimension %q", issue.Dimension)
		}
		switch issue.Severity {
		case types.SeverityInfo, types.SeverityWarning, types.SeverityCritical:
		default:
			return types.ValidationReport{}, fmt.Errorf("unknown validation severity %q", issue.Severity)
		}
		envelope.Issues[index].Message = truncateValidationText(issue.Message, 400)
	}
	report := types.ValidationReport{
		ID:                uuid.NewString(),
		Model:             identity,
		FactScore:         *envelope.FactScore,
		LogicScore:        *envelope.LogicScore,
		CitationScore:     *envelope.CitationScore,
		CompletenessScore: *envelope.CompletenessScore,
		Issues:            envelope.Issues,
	}
	if err := report.Validate(); err != nil {
		return types.ValidationReport{}, err
	}
	return report, nil
}

func truncateValidationText(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

func (p *PluginVerifiedAnswer) modelIdentities(ctx context.Context, chatManage *types.ChatManage) []types.ModelIdentity {
	ids := verificationModelIDs(chatManage)
	seen := map[string]bool{}
	result := make([]types.ModelIdentity, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if p.modelService != nil {
			model, err := p.modelService.GetModelByID(ctx, id)
			if err == nil && model != nil {
				result = append(result, modelIdentityFromRecord(model))
				continue
			}
		}
		result = append(result, types.NormalizeModelIdentity("model", "local://"+id, id, ""))
	}
	if len(result) == 0 {
		result = append(result, types.NormalizeModelIdentity("pipeline", "local://deterministic-validator", "deterministic-validator", "1"))
	}
	return result
}

func reportModelKeys(reports []types.ValidationReport) []string {
	keys := make([]string, 0, len(reports))
	for _, report := range reports {
		keys = append(keys, report.Model.Key())
	}
	return keys
}

func emitVerificationStage(ctx context.Context, chatManage *types.ChatManage, stage types.VerificationStageEvent) {
	if chatManage == nil || chatManage.EventBus == nil {
		return
	}
	_ = chatManage.EventBus.Emit(ctx, types.Event{Type: types.EventType(event.EventAgentReflection), SessionID: chatManage.SessionID, Data: stage})
}
