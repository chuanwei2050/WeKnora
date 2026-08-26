package chatpipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
)

// PluginQueryUnderstand performs query rewriting and intent classification.
// It uses conversation history and an LLM to optimise the user's original query
// and determine the downstream pipeline behaviour.
type PluginQueryUnderstand struct {
	modelService   interfaces.ModelService
	messageService interfaces.MessageService
	config         *config.Config
}

var (
	rewriteImageSepPattern  = regexp.MustCompile(`(?s)^(.*?)\s*\n?---\n(.*)$`)
	simpleArithmeticPattern = regexp.MustCompile(`^[零〇一二三四五六七八九十百千万亿两\d０-９.+\-*/×÷加减乘除()（）]+$`)
)

type queryUnderstandOutput struct {
	RewriteQuery        string                 `json:"rewrite_query"`
	Intent              types.QueryIntent      `json:"intent"`
	ImageDescription    string                 `json:"image_description"`
	ComplexityLevel     types.ComplexityLevel  `json:"complexity_level"`
	ReasoningSubtype    types.ReasoningSubtype `json:"reasoning_subtype"`
	NeedsEntityRelation bool                   `json:"needs_entity_relation"`
	Confidence          float64                `json:"confidence"`
	RationaleSummary    string                 `json:"rationale_summary"`
}

// NewPluginQueryUnderstand creates a new query-understanding plugin instance
// and registers it with the event manager.
func NewPluginQueryUnderstand(eventManager *EventManager,
	modelService interfaces.ModelService, messageService interfaces.MessageService,
	config *config.Config,
) *PluginQueryUnderstand {
	res := &PluginQueryUnderstand{
		modelService:   modelService,
		messageService: messageService,
		config:         config,
	}
	eventManager.Register(res)
	return res
}

// ClassifyQuery applies the same strict routing classifier used by the normal
// RAG pipeline to another entry point, such as AgentQA. The caller supplies a
// request-scoped ChatManage so the returned RoutingDecision is identical in
// shape and policy to the normal pipeline decision.
func ClassifyQuery(ctx context.Context, model chat.Chat, appConfig *config.Config, chatManage *types.ChatManage) (*types.RoutingDecision, error) {
	if chatManage == nil {
		return nil, fmt.Errorf("chat manage is required")
	}
	if !chatManage.ComplexityRouting.Enabled {
		return nil, fmt.Errorf("complexity routing is disabled")
	}
	if model == nil {
		decision := conservativeRoutingDecision(chatManage, types.DegradationMissingCapability)
		return &decision, fmt.Errorf("routing model is required")
	}
	if appConfig == nil || appConfig.Conversation == nil {
		decision := conservativeRoutingDecision(chatManage, types.DegradationMissingCapability)
		return &decision, fmt.Errorf("conversation config is required")
	}

	plugin := &PluginQueryUnderstand{config: appConfig}
	systemContent, userContent := plugin.buildPrompts(chatManage, chatManage.History)
	thinking := false
	started := time.Now()
	response, err := model.Chat(ctx, []chat.Message{
		{Role: "system", Content: systemContent},
		{Role: "user", Content: userContent},
	}, &chat.ChatOptions{Temperature: 0.3, MaxCompletionTokens: 150, Thinking: &thinking})
	if err != nil {
		decision := conservativeRoutingDecision(chatManage, types.DegradationMissingCapability)
		return &decision, fmt.Errorf("classify query: %w", err)
	}
	if response == nil {
		decision := conservativeRoutingDecision(chatManage, types.DegradationMissingCapability)
		return &decision, fmt.Errorf("classify query: empty model response")
	}
	output, err := parseStrictRoutingOutput(response.Content)
	if err != nil {
		decision := conservativeRoutingDecision(chatManage, types.DegradationParseFailed)
		decision.ClassificationMillis = time.Since(started).Milliseconds()
		return &decision, err
	}
	applyQueryUnderstandOutput(chatManage, output, true)
	if chatManage.RoutingDecision == nil {
		return nil, fmt.Errorf("routing decision was not produced")
	}
	chatManage.RoutingDecision.ClassificationMillis = time.Since(started).Milliseconds()
	return chatManage.RoutingDecision, nil
}

// BuildSubQuestionPlan performs the bounded decomposition step for complex
// Agent queries. Classification remains a separate strict contract so normal
// RAG and Agent still share the same routing decision.
func BuildSubQuestionPlan(ctx context.Context, model chat.Chat, query string, complexity types.QuestionComplexity, maxQuestions, maxCalls int, maxDurationMs int64) (types.SubQuestionPlan, error) {
	if maxQuestions <= 0 {
		maxQuestions = 4
	}
	if maxCalls <= 0 {
		maxCalls = maxQuestions
	}
	if maxDurationMs <= 0 {
		maxDurationMs = 30000
	}
	if model == nil {
		return types.PlanSubQuestions(query, complexity, maxQuestions, maxCalls, maxDurationMs)
	}
	prompt := fmt.Sprintf(`将用户问题拆解为有序、有限的检索子问题。只输出一个 JSON 对象，不要 Markdown、解释或思维过程。
每个子问题字段为 index、query、depends_on、required；index 从 1 开始，depends_on 只能引用更早的 index。
仅在确实需要多步证据时返回多个子问题；简单问题返回一个子问题。后续子问题不得使用未解析的代词，必须能结合原问题和前序结果独立检索。
最多 %d 个子问题。原问题：%s`, maxQuestions, strings.TrimSpace(query))
	thinking := false
	response, err := model.Chat(ctx, []chat.Message{{Role: "system", Content: prompt}}, &chat.ChatOptions{Temperature: 0, MaxCompletionTokens: 400, Thinking: &thinking})
	if err != nil || response == nil {
		return types.PlanSubQuestions(query, complexity, maxQuestions, maxCalls, maxDurationMs)
	}
	return types.ParseSubQuestionPlan(response.Content, query, maxQuestions, maxCalls, maxDurationMs)
}

func conservativeRoutingDecision(chatManage *types.ChatManage, reason types.DegradationReason) types.RoutingDecision {
	decision := types.PlanRouting(conservativeRoutingClassification(chatManage), chatManage.ComplexityRouting)
	if reason != "" {
		decision.DegradationReason = reason
	}
	chatManage.RoutingDecision = &decision
	chatManage.ApplyRoutingDecision()
	return decision
}

func conservativeRoutingClassification(chatManage *types.ChatManage) types.QuestionComplexity {
	if chatManage != nil && (types.NeedsEntityRelation(chatManage.Query) || types.NeedsEntityRelation(chatManage.RewriteQuery)) {
		// A parse failure must not erase an explicit relation request. This is a
		// deterministic boundary hint, not a model-derived confidence score.
		return types.QuestionComplexity{
			Level: types.ComplexityL3, Subtype: types.SubtypeMultiHop,
			NeedsEntityRelation: true, Confidence: 1,
			RationaleSummary: "问题明确包含实体关系或多跳查询信号",
		}
	}
	return types.QuestionComplexity{}
}

// ActivationEvents returns the list of event types this plugin responds to.
func (p *PluginQueryUnderstand) ActivationEvents() []types.EventType {
	return []types.EventType{types.QUERY_UNDERSTAND}
}

// OnEvent processes triggered events.
// Handles three input combinations:
//   - Text only: standard rewrite + intent classification (uses chat model)
//   - Text + images: multimodal rewrite + intent + image description (uses VLM/vision model)
//   - Images only: multimodal analysis + intent + image description (uses VLM/vision model)
func (p *PluginQueryUnderstand) OnEvent(ctx context.Context,
	eventType types.EventType, chatManage *types.ChatManage, next func() *PluginError,
) *PluginError {
	chatManage.RewriteQuery = chatManage.Query
	stageID, stageStarted := emitPipelineStageStart(ctx, chatManage, "query_understand", "理解问题")
	stageSuccess := false
	finishStage := func() {
		status := "failed"
		output := "问题理解未完成"
		if stageSuccess {
			status = "completed"
			output = "已理解问题"
		}
		emitPipelineStageResult(ctx, chatManage, stageID, "query_understand", output, stageStarted, stageSuccess, map[string]interface{}{"status": status})
	}

	hasImages := len(chatManage.Images) > 0
	needRewrite := chatManage.EnableRewrite
	needRouting := chatManage.ComplexityRouting.Enabled
	if !needRewrite && !hasImages && !needRouting {
		stageSuccess = true
		finishStage()
		pipelineInfo(ctx, "QueryUnderstand", "skip", map[string]interface{}{
			"session_id": chatManage.SessionID,
			"reason":     "rewrite_disabled_no_images",
		})
		return next()
	}

	pipelineInfo(ctx, "QueryUnderstand", "input", map[string]interface{}{
		"session_id":     chatManage.SessionID,
		"tenant_id":      chatManage.TenantID,
		"user_query":     chatManage.Query,
		"has_images":     hasImages,
		"enable_rewrite": chatManage.EnableRewrite,
	})

	// --- Load and prepare conversation history ---
	var historyList []*types.History
	if len(chatManage.History) > 0 {
		historyList = chatManage.History
		pipelineInfo(ctx, "QueryUnderstand", "history_reused", map[string]interface{}{
			"session_id": chatManage.SessionID,
			"rounds":     len(historyList),
		})
	} else {
		historyList = p.loadHistory(ctx, chatManage)
	}
	if isSimpleExplicitFactQuery(chatManage, historyList) {
		chatManage.Intent = types.IntentKBSearch
		if needRouting {
			decision := types.PlanRouting(types.QuestionComplexity{Level: types.ComplexityL1, Subtype: types.SubtypeExplicitFact, Confidence: 1, RationaleSummary: "单轮明确事实查询"}, chatManage.ComplexityRouting)
			chatManage.RoutingDecision = &decision
			chatManage.ApplyRoutingDecision()
		}
		pipelineInfo(ctx, "QueryUnderstand", "fast_path", map[string]interface{}{"session_id": chatManage.SessionID, "query": chatManage.Query})
		stageSuccess = true
		finishStage()
		return next()
	}

	// --- Select the appropriate model ---
	rewriteModel, useImages := p.selectModel(ctx, chatManage, hasImages)
	if rewriteModel == nil {
		if needRouting {
			decision := conservativeRoutingDecision(chatManage, types.DegradationMissingCapability)
			pipelineInfo(ctx, "QueryUnderstand", "routing_degraded", map[string]interface{}{
				"session_id": chatManage.SessionID,
				"routing":    routingSummary(&decision),
			})
		}
		pipelineError(ctx, "QueryUnderstand", "get_model", map[string]interface{}{
			"session_id": chatManage.SessionID,
		})
		finishStage()
		return next()
	}

	// --- Build prompts ---
	systemContent, userContent := p.buildPrompts(chatManage, historyList)

	userMsg := chat.Message{Role: "user", Content: userContent}
	if useImages {
		userMsg.Images = chatManage.Images
	}

	maxTokens := 150
	if useImages {
		maxTokens = 500
	}

	// --- Emit progress event for image analysis ---
	var toolCallID string
	if useImages && chatManage.EventBus != nil {
		toolCallID = uuid.New().String()
		chatManage.EventBus.Emit(ctx, types.Event{
			Type:      types.EventType(event.EventAgentToolCall),
			SessionID: chatManage.SessionID,
			Data: event.AgentToolCallData{
				ToolCallID: toolCallID,
				ToolName:   "image_analysis",
			},
		})
	}

	// --- Call model ---
	thinking := false
	vlmStart := time.Now()
	routingStart := vlmStart
	response, err := rewriteModel.Chat(ctx, []chat.Message{
		{Role: "system", Content: systemContent},
		userMsg,
	}, &chat.ChatOptions{
		Temperature:         0.3,
		MaxCompletionTokens: maxTokens,
		Thinking:            &thinking,
	})
	if err != nil {
		if toolCallID != "" && chatManage.EventBus != nil {
			chatManage.EventBus.Emit(ctx, types.Event{
				Type:      types.EventType(event.EventAgentToolResult),
				SessionID: chatManage.SessionID,
				Data: event.AgentToolResultData{
					ToolCallID: toolCallID,
					ToolName:   "image_analysis",
					Output:     "图片分析失败",
					Success:    false,
					Duration:   time.Since(vlmStart).Milliseconds(),
				},
			})
		}
		pipelineError(ctx, "QueryUnderstand", "model_call", map[string]interface{}{
			"session_id": chatManage.SessionID,
			"error":      err.Error(),
		})
		if needRouting {
			decision := conservativeRoutingDecision(chatManage, types.DegradationMissingCapability)
			pipelineInfo(ctx, "QueryUnderstand", "routing_degraded", map[string]interface{}{
				"session_id": chatManage.SessionID,
				"routing":    routingSummary(&decision),
			})
		}
		finishStage()
		return next()
	}
	if response == nil {
		if needRouting {
			decision := conservativeRoutingDecision(chatManage, types.DegradationMissingCapability)
			pipelineInfo(ctx, "QueryUnderstand", "routing_degraded", map[string]interface{}{
				"session_id": chatManage.SessionID,
				"routing":    routingSummary(&decision),
			})
		}
		pipelineError(ctx, "QueryUnderstand", "empty_model_response", map[string]interface{}{
			"session_id": chatManage.SessionID,
		})
		finishStage()
		return next()
	}

	// --- Emit completion event for image analysis ---
	if toolCallID != "" && chatManage.EventBus != nil {
		chatManage.EventBus.Emit(ctx, types.Event{
			Type:      types.EventType(event.EventAgentToolResult),
			SessionID: chatManage.SessionID,
			Data: event.AgentToolResultData{
				ToolCallID: toolCallID,
				ToolName:   "image_analysis",
				Output:     "已分析图片内容",
				Success:    true,
				Duration:   time.Since(vlmStart).Milliseconds(),
			},
		})
	}

	// --- Parse structured output ---
	p.parseOutput(chatManage, response.Content)
	if chatManage.RoutingDecision != nil {
		chatManage.RoutingDecision.ClassificationMillis = time.Since(routingStart).Milliseconds()
	}

	// Persist image description asynchronously — this DB write does not affect
	// the current pipeline result, so it can run in the background.
	if chatManage.ImageDescription != "" && chatManage.UserMessageID != "" {
		go p.updateUserMessageImageCaption(context.WithoutCancel(ctx), chatManage)
	}

	// --- Apply intent-specific system prompt override ---
	if !chatManage.NeedsRetrieval() {
		if prompt, ok := p.config.Conversation.IntentSystemPrompts[string(chatManage.Intent)]; ok {
			chatManage.SystemPromptOverride = prompt
			pipelineInfo(ctx, "QueryUnderstand", "prompt_override", map[string]interface{}{
				"session_id": chatManage.SessionID,
				"intent":     chatManage.Intent,
			})
		}
	}

	pipelineInfo(ctx, "QueryUnderstand", "output", map[string]interface{}{
		"session_id":          chatManage.SessionID,
		"rewrite_query":       chatManage.RewriteQuery,
		"intent":              chatManage.Intent,
		"has_image_desc":      chatManage.ImageDescription != "",
		"has_prompt_override": chatManage.SystemPromptOverride != "",
		"original_output":     response.Content,
		"routing":             routingSummary(chatManage.RoutingDecision),
	})
	stageSuccess = true
	finishStage()
	return next()
}

func isSimpleExplicitFactQuery(chatManage *types.ChatManage, history []*types.History) bool {
	if chatManage == nil || len(history) > 0 || len(chatManage.Images) > 0 || chatManage.WebSearchEnabled {
		return false
	}
	if len(chatManage.SearchTargets) == 0 && len(chatManage.KnowledgeBaseIDs) == 0 && len(chatManage.KnowledgeIDs) == 0 {
		return false
	}
	query := strings.TrimSpace(chatManage.Query)
	length := len([]rune(query))
	if length < 2 || length > 40 {
		return false
	}
	if isUtilityQuery(query) {
		return false
	}
	for _, marker := range []string{"为什么", "如何", "怎么", "比较", "对比", "区别", "关系", "影响", "原因", "方案", "分析", "总结", "它", "他是", "她是", "这个", "那个", "上述", "上面", "前面", "刚才"} {
		if strings.Contains(query, marker) {
			return false
		}
	}
	for _, marker := range []string{"谁有", "谁具备", "谁持有", "有哪些", "是否有", "有无"} {
		if strings.Contains(query, marker) {
			return true
		}
	}
	return false
}

func isUtilityQuery(query string) bool {
	for _, marker := range []string{"天气", "气温", "下雨", "降雨", "几级风"} {
		if strings.Contains(query, marker) {
			return true
		}
	}
	if strings.Contains(query, "多少度") {
		return true
	}

	expression := strings.NewReplacer(
		"多少", "", "等于", "", "是", "", "几", "", "？", "", "?", "", " ", "",
	).Replace(query)
	return expression != "" && simpleArithmeticPattern.MatchString(expression)
}

// updateUserMessageImageCaption writes the generated ImageDescription back to
// the stored user message so that subsequent turns can see it in history.
func (p *PluginQueryUnderstand) updateUserMessageImageCaption(ctx context.Context, chatManage *types.ChatManage) {
	msg, err := p.messageService.GetMessage(ctx, chatManage.SessionID, chatManage.UserMessageID)
	if err != nil {
		pipelineWarn(ctx, "QueryUnderstand", "get_user_message", map[string]interface{}{
			"session_id":      chatManage.SessionID,
			"user_message_id": chatManage.UserMessageID,
			"error":           err.Error(),
		})
		return
	}

	if len(msg.Images) == 0 {
		return
	}

	msg.Images[0].Caption = chatManage.ImageDescription

	if err := p.messageService.UpdateMessageImages(ctx, chatManage.SessionID, chatManage.UserMessageID, msg.Images); err != nil {
		pipelineWarn(ctx, "QueryUnderstand", "update_image_caption", map[string]interface{}{
			"session_id":      chatManage.SessionID,
			"user_message_id": chatManage.UserMessageID,
			"error":           err.Error(),
		})
	}
}

// loadHistory fetches and processes conversation history for rewrite context.
func (p *PluginQueryUnderstand) loadHistory(ctx context.Context, chatManage *types.ChatManage) []*types.History {
	maxRounds := p.config.Conversation.MaxRounds
	if chatManage.MaxRounds > 0 {
		maxRounds = chatManage.MaxRounds
	}

	historyList, err := loadAndProcessHistory(ctx, p.messageService, chatManage.SessionID, maxRounds, 20)
	if err != nil {
		pipelineWarn(ctx, "QueryUnderstand", "history_fetch", map[string]interface{}{
			"session_id": chatManage.SessionID,
			"error":      err.Error(),
		})
		return nil
	}

	chatManage.History = historyList

	if len(historyList) > 0 {
		pipelineInfo(ctx, "QueryUnderstand", "history_ready", map[string]interface{}{
			"session_id":     chatManage.SessionID,
			"history_rounds": len(historyList),
		})
	}

	return historyList
}

// selectModel picks the model for query understanding. When images are present
// it prefers a vision-capable model. Returns (model, useImages).
func (p *PluginQueryUnderstand) selectModel(ctx context.Context, chatManage *types.ChatManage, hasImages bool) (chat.Chat, bool) {
	if hasImages {
		if chatManage.ChatModelSupportsVision {
			m, err := p.modelService.GetChatModel(ctx, chatManage.ChatModelID)
			if err == nil {
				return m, true
			}
			pipelineWarn(ctx, "QueryUnderstand", "vision_model_fallback", map[string]interface{}{
				"session_id": chatManage.SessionID,
				"error":      err.Error(),
			})
		}
		if chatManage.VLMModelID != "" {
			m, err := p.modelService.GetChatModel(ctx, chatManage.VLMModelID)
			if err == nil {
				return m, true
			}
			pipelineWarn(ctx, "QueryUnderstand", "vlm_model_fallback", map[string]interface{}{
				"session_id":   chatManage.SessionID,
				"vlm_model_id": chatManage.VLMModelID,
				"error":        err.Error(),
			})
		}
		pipelineWarn(ctx, "QueryUnderstand", "no_vision_model", map[string]interface{}{
			"session_id": chatManage.SessionID,
		})
	}

	m, err := p.modelService.GetChatModel(ctx, chatManage.ChatModelID)
	if err != nil {
		pipelineError(ctx, "QueryUnderstand", "get_model", map[string]interface{}{
			"session_id":    chatManage.SessionID,
			"chat_model_id": chatManage.ChatModelID,
			"error":         err.Error(),
		})
		return nil, false
	}
	return m, false
}

// buildPrompts constructs system and user prompts with placeholder replacement.
func (p *PluginQueryUnderstand) buildPrompts(chatManage *types.ChatManage, historyList []*types.History) (string, string) {
	userPrompt := p.config.Conversation.RewritePromptUser
	if chatManage.RewritePromptUser != "" {
		userPrompt = chatManage.RewritePromptUser
	}
	systemPrompt := p.config.Conversation.RewritePromptSystem
	if chatManage.RewritePromptSystem != "" {
		systemPrompt = chatManage.RewritePromptSystem
	}

	conversationText := formatConversationHistory(historyList)

	queryContent := chatManage.Query
	if len(chatManage.Images) > 0 {
		queryContent += fmt.Sprintf("\n\n<images_uploaded count=\"%d\" />", len(chatManage.Images))
	} else {
		queryContent += "\n\n<no_image_attached />"
	}
	if len(chatManage.Attachments) > 0 {
		queryContent += chatManage.Attachments.BuildPrompt()
	} else {
		queryContent += "\n<no_document_attached />"
	}
	if chatManage.ComplexityRouting.Enabled {
		budget := chatManage.ComplexityRouting.InputBudgetChars
		if budget <= 0 {
			budget = 12000
		}
		conversationText = truncatePromptInput(conversationText, budget/2)
		queryContent = truncatePromptInput(queryContent, budget)
	}

	vals := types.PlaceholderValues{
		"conversation": conversationText,
		"query":        queryContent,
		"language":     chatManage.Language,
	}
	if chatManage.ComplexityRouting.Enabled {
		systemPrompt += "\nReturn JSON fields complexity_level (L1/L2/L3/L4), reasoning_subtype, needs_entity_relation (true only when entity relations, hierarchy, or multi-hop graph reasoning is needed), confidence (0..1), and rationale_summary (one short sentence, no chain-of-thought)."
		systemPrompt = AppendComplexityFewShotExamples(systemPrompt, chatManage.ComplexityRouting.FewShot, defaultComplexityFewShotLimit)
	}

	return types.RenderPromptPlaceholders(systemPrompt, vals),
		types.RenderPromptPlaceholders(userPrompt, vals)
}

func truncatePromptInput(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "\n[input truncated]"
}

// parseOutput extracts the rewritten query, intent classification, and optional
// image description from the model's structured JSON output.
//
// Expected format: {"rewrite_query":"...","intent":"kb_search","image_description":"..."}
func (p *PluginQueryUnderstand) parseOutput(chatManage *types.ChatManage, raw string) {
	content := strings.TrimSpace(raw)
	if content == "" {
		return
	}

	if chatManage.ComplexityRouting.Enabled {
		output, err := parseStrictRoutingOutput(content)
		if err != nil {
			// Keep the ordinary rewrite/intent result when it is recoverable, but
			// never accept an unvalidated routing decision.
			if fallbackOutput, ok := parseStructuredQueryOutput(content); ok {
				applyQueryUnderstandOutput(chatManage, fallbackOutput, false)
			}
			decision := types.PlanRouting(conservativeRoutingClassification(chatManage), chatManage.ComplexityRouting)
			decision.DegradationReason = types.DegradationParseFailed
			chatManage.RoutingDecision = &decision
			chatManage.ApplyRoutingDecision()
			return
		}
		applyQueryUnderstandOutput(chatManage, output, true)
		return
	}

	if output, ok := parseStructuredQueryOutput(content); ok {
		applyQueryUnderstandOutput(chatManage, output, false)
		return
	}

	// If JSON parsing failed entirely, treat the raw text as the rewritten query
	// and default to IntentKBSearch for safety.
	if content != "" {
		chatManage.RewriteQuery = content
	}
}

func applyQueryUnderstandOutput(chatManage *types.ChatManage, output queryUnderstandOutput, applyRouting bool) {
	if rewrite := strings.TrimSpace(output.RewriteQuery); rewrite != "" {
		chatManage.RewriteQuery = rewrite
	}
	if output.Intent != "" {
		chatManage.Intent = output.Intent
	}
	chatManage.ImageDescription = strings.TrimSpace(output.ImageDescription)
	if applyRouting {
		decision := types.PlanRouting(types.QuestionComplexity{
			Level: output.ComplexityLevel, Subtype: output.ReasoningSubtype,
			NeedsEntityRelation: output.NeedsEntityRelation,
			Confidence:          output.Confidence, RationaleSummary: output.RationaleSummary,
		}, chatManage.ComplexityRouting)
		chatManage.RoutingDecision = &decision
		chatManage.ApplyRoutingDecision()
	}
}

// parseStrictRoutingOutput accepts only a single JSON object with validated
// routing fields. Model prose, markdown fences, missing fields and wrong JSON
// types deliberately fail closed so the caller can use the deterministic
// conservative route.
func parseStrictRoutingOutput(raw string) (queryUnderstandOutput, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil || fields == nil {
		if err == nil {
			err = fmt.Errorf("expected a JSON object")
		}
		return queryUnderstandOutput{}, fmt.Errorf("strict routing JSON: %w", err)
	}

	readString := func(name string, required bool) (string, error) {
		value, ok := fields[name]
		if !ok {
			if required {
				return "", fmt.Errorf("missing field %q", name)
			}
			return "", nil
		}
		var result string
		if err := json.Unmarshal(value, &result); err != nil || strings.TrimSpace(result) == "" {
			if err == nil {
				err = fmt.Errorf("must be a non-empty string")
			}
			return "", fmt.Errorf("field %q: %w", name, err)
		}
		return strings.TrimSpace(result), nil
	}

	level, err := readString("complexity_level", true)
	if err != nil {
		return queryUnderstandOutput{}, err
	}
	subtype, err := readString("reasoning_subtype", true)
	if err != nil {
		return queryUnderstandOutput{}, err
	}
	needsEntityRelationRaw, ok := fields["needs_entity_relation"]
	if !ok || string(needsEntityRelationRaw) == "null" {
		return queryUnderstandOutput{}, fmt.Errorf("missing field %q", "needs_entity_relation")
	}
	var needsEntityRelation bool
	if err := json.Unmarshal(needsEntityRelationRaw, &needsEntityRelation); err != nil {
		return queryUnderstandOutput{}, fmt.Errorf("field %q: %w", "needs_entity_relation", err)
	}
	rationale, err := readString("rationale_summary", false)
	if err != nil {
		return queryUnderstandOutput{}, err
	}
	rewrite, err := readString("rewrite_query", false)
	if err != nil {
		return queryUnderstandOutput{}, err
	}
	intent, err := readString("intent", false)
	if err != nil {
		return queryUnderstandOutput{}, err
	}
	desc := firstStringField(fields,
		"image_description", "image_desc", "image_text", "image_ocr_text", "description")
	ocr := firstStringField(fields, "ocr_text", "ocr", "full_ocr", "image_ocr", "ocr_content")
	imageDescription, _ := mergeImageDescAndOCR(strings.TrimSpace(desc), strings.TrimSpace(ocr))

	confidenceRaw, ok := fields["confidence"]
	if !ok || string(confidenceRaw) == "null" {
		return queryUnderstandOutput{}, fmt.Errorf("missing field %q", "confidence")
	}
	var confidence float64
	if err := json.Unmarshal(confidenceRaw, &confidence); err != nil {
		return queryUnderstandOutput{}, fmt.Errorf("field %q: %w", "confidence", err)
	}

	output := queryUnderstandOutput{
		RewriteQuery: rewrite, Intent: types.QueryIntent(intent),
		ComplexityLevel: types.ComplexityLevel(level), ReasoningSubtype: types.ReasoningSubtype(subtype),
		NeedsEntityRelation: needsEntityRelation,
		Confidence:          confidence, RationaleSummary: rationale, ImageDescription: imageDescription,
	}
	if err := (types.QuestionComplexity{
		Level: output.ComplexityLevel, Subtype: output.ReasoningSubtype,
		NeedsEntityRelation: output.NeedsEntityRelation,
		Confidence:          output.Confidence, RationaleSummary: output.RationaleSummary,
	}).Validate(); err != nil {
		return queryUnderstandOutput{}, err
	}
	return output, nil
}

func parseStructuredQueryOutput(raw string) (queryUnderstandOutput, bool) {
	content := strings.TrimSpace(raw)
	if content == "" {
		return queryUnderstandOutput{}, false
	}

	if parsed, ok := parseStructuredQueryOutputJSON(content); ok {
		return parsed, true
	}

	// Be tolerant to occasional markdown wrappers or extra prose.
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return queryUnderstandOutput{}, false
	}
	candidate := content[start : end+1]
	if parsed, ok := parseStructuredQueryOutputJSON(candidate); ok {
		return parsed, true
	}
	return queryUnderstandOutput{}, false
}

func parseStructuredQueryOutputJSON(content string) (queryUnderstandOutput, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &obj); err != nil {
		return queryUnderstandOutput{}, false
	}

	out := queryUnderstandOutput{
		RewriteQuery: strings.TrimSpace(firstStringField(obj,
			"rewrite_query", "rewritten_query", "query", "question")),
	}
	if raw, ok := obj["complexity_level"]; ok {
		_ = json.Unmarshal(raw, &out.ComplexityLevel)
	}
	if raw, ok := obj["reasoning_subtype"]; ok {
		_ = json.Unmarshal(raw, &out.ReasoningSubtype)
	}
	if raw, ok := obj["confidence"]; ok {
		_ = json.Unmarshal(raw, &out.Confidence)
	}
	if raw, ok := obj["needs_entity_relation"]; ok {
		_ = json.Unmarshal(raw, &out.NeedsEntityRelation)
	}
	out.RationaleSummary = firstStringField(obj, "rationale_summary", "routing_rationale")

	intentStr := strings.TrimSpace(firstStringField(obj, "intent"))
	if intentStr != "" {
		out.Intent = types.QueryIntent(intentStr)
	}

	desc := strings.TrimSpace(firstStringField(obj,
		"image_description", "image_desc", "image_text", "image_ocr_text", "description"))
	ocr := strings.TrimSpace(firstStringField(obj,
		"ocr_text", "ocr", "full_ocr", "image_ocr", "ocr_content"))
	combined, set := mergeImageDescAndOCR(desc, ocr)
	if set {
		out.ImageDescription = combined
	}

	return out, true
}

func routingSummary(decision *types.RoutingDecision) map[string]interface{} {
	if decision == nil {
		return nil
	}
	return decision.Summary()
}

func firstStringField(obj map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		raw, ok := obj[key]
		if !ok || len(raw) == 0 {
			continue
		}

		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
	}
	return ""
}

func mergeImageDescAndOCR(desc, ocr string) (string, bool) {
	if desc == "" && ocr == "" {
		return "", false
	}
	if desc == "" {
		return ocr, true
	}
	if ocr == "" {
		return desc, true
	}
	if strings.Contains(desc, ocr) {
		return desc, true
	}
	return desc + "\n\n[OCR]\n" + ocr, true
}

// formatConversationHistory formats conversation history for prompt template.
func formatConversationHistory(historyList []*types.History) string {
	if len(historyList) == 0 {
		return ""
	}

	var builder strings.Builder
	for _, h := range historyList {
		builder.WriteString("------BEGIN------\n")
		builder.WriteString("User question: ")
		builder.WriteString(h.Query)
		builder.WriteString("\nAssistant answer: ")
		builder.WriteString(h.Answer)
		builder.WriteString("\n------END------\n")
	}
	return builder.String()
}
