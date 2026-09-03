package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	chatpipeline "github.com/Tencent/WeKnora/internal/application/service/chat_pipeline"
	"github.com/Tencent/WeKnora/internal/common"
	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// KnowledgeQA performs knowledge base question answering with LLM summarization
// Events are emitted through eventBus (references, answer chunks, completion)
// customAgent is optional - if provided, uses custom agent configuration for multiTurnEnabled and historyTurns
func (s *sessionService) KnowledgeQA(
	ctx context.Context,
	req *types.QARequest,
	eventBus *event.EventBus,
) error {
	logger.Infof(
		ctx,
		"Knowledge base question answering parameters, session ID: %s, query: %s, webSearchEnabled: %v, enableMemory: %v",
		req.Session.ID,
		req.Query,
		req.WebSearchEnabled,
		req.EnableMemory,
	)

	// Resolve knowledge bases using shared helper
	knowledgeBaseIDs, knowledgeIDs := s.resolveKnowledgeBases(ctx, req)

	// Resolve chat model ID using shared helper
	chatModelID, err := s.resolveChatModelID(ctx, req, knowledgeBaseIDs, knowledgeIDs)
	if err != nil {
		return err
	}

	// Initialize ChatManage defaults from config.yaml. Platform conversation
	// settings override these values below for each new request.
	summaryConfig := types.SummaryConfig{
		Prompt:              s.cfg.Conversation.Summary.Prompt,
		ContextTemplate:     s.cfg.Conversation.Summary.ContextTemplate,
		Temperature:         s.cfg.Conversation.Summary.Temperature,
		NoMatchPrefix:       s.cfg.Conversation.Summary.NoMatchPrefix,
		MaxCompletionTokens: s.cfg.Conversation.Summary.MaxCompletionTokens,
		Thinking:            nil,
	}
	// Resolve chat model vision capability and VLM model ID for image routing
	var chatModelSupportsVision bool
	var vlmModelID string
	if chatModelID != "" {
		if chatModelInfo, err := s.modelService.GetModelByID(ctx, chatModelID); err == nil && chatModelInfo != nil {
			chatModelSupportsVision = chatModelInfo.Parameters.SupportsVision
			modelThinking := chatModelInfo.Parameters.Thinking
			summaryConfig.Thinking = &modelThinking
		}
	}
	if model, defaultErr := s.modelService.GetDefaultModel(ctx, types.ModelTypeVLLM, "vlm"); defaultErr == nil {
		vlmModelID = model.ID
	}
	var rerankModelID string
	if model, defaultErr := s.modelService.GetDefaultModel(ctx, types.ModelTypeRerank, "rerank"); defaultErr == nil {
		rerankModelID = model.ID
	}

	// Resolve retrieval tenant scope using shared helper
	retrievalTenantID := s.resolveRetrievalTenantID(ctx, req)
	retrievalConfig := s.effectiveRetrievalConfig(ctx, retrievalTenantID)
	conversationConfig := s.effectiveConversationConfig(ctx, retrievalTenantID)
	fallbackStrategy := types.FallbackStrategy(s.cfg.Conversation.FallbackStrategy)
	fallbackResponse := s.cfg.Conversation.FallbackResponse
	fallbackPrompt := s.cfg.Conversation.FallbackPrompt
	if conversationConfig != nil {
		fallbackStrategy = types.FallbackStrategy(conversationConfig.FallbackStrategy)
		fallbackResponse = conversationConfig.FallbackResponse
		fallbackPrompt = conversationConfig.FallbackPrompt
	}
	if fallbackStrategy == "" {
		fallbackStrategy = types.FallbackStrategyFixed
		logger.Infof(ctx, "Fallback strategy not set, using default: %v", fallbackStrategy)
	}

	// Build unified search targets (computed once, used throughout pipeline)
	searchTargets, err := s.buildSearchTargets(ctx, retrievalTenantID, knowledgeBaseIDs, knowledgeIDs, req.FilterDisabledFolders)
	if err != nil {
		logger.Warnf(ctx, "Failed to build search targets: %v", err)
	}

	// Create chat management object with session settings
	logger.Infof(
		ctx,
		"Creating chat manage object, knowledge base IDs: %v, knowledge IDs: %v, chat model ID: %s, search targets: %d",
		knowledgeBaseIDs,
		knowledgeIDs,
		chatModelID,
		len(searchTargets),
	)

	// Get UserID from context
	userID, _ := types.UserIDFromContext(ctx)

	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			Query:                   req.Query,
			SessionID:               req.Session.ID,
			UserID:                  userID,
			EnableMemory:            req.EnableMemory,
			MaxRounds:               s.cfg.Conversation.MaxRounds,
			KnowledgeBaseIDs:        knowledgeBaseIDs,
			KnowledgeIDs:            knowledgeIDs,
			SearchTargets:           searchTargets,
			VectorThreshold:         retrievalConfig.VectorThreshold,
			KeywordThreshold:        retrievalConfig.KeywordThreshold,
			EmbeddingTopK:           retrievalConfig.EmbeddingTopK,
			VectorRecallTopK:        retrievalConfig.VectorRecallTopK,
			KeywordRecallTopK:       retrievalConfig.KeywordRecallTopK,
			RRFVectorWeight:         retrievalConfig.RRFVectorWeight,
			RerankCandidateTopK:     retrievalConfig.RerankCandidateTopK,
			RerankTopK:              retrievalConfig.RerankTopK,
			RerankThreshold:         retrievalConfig.RerankThreshold,
			ChatModelID:             chatModelID,
			RerankModelID:           rerankModelID,
			SummaryConfig:           summaryConfig,
			FallbackStrategy:        fallbackStrategy,
			FallbackResponse:        fallbackResponse,
			FallbackPrompt:          fallbackPrompt,
			EnableRewrite:           s.cfg.Conversation.EnableRewrite,
			EnableQueryExpansion:    retrievalConfig.EnableQueryExpansion,
			RewritePromptSystem:     s.cfg.Conversation.RewritePromptSystem,
			RewritePromptUser:       s.cfg.Conversation.RewritePromptUser,
			WebSearchEnabled:        req.WebSearchEnabled,
			WebSearchProviderID:     s.resolveWebSearchProviderID(ctx, req, retrievalTenantID),
			WebSearchMaxResults:     s.resolveWebSearchMaxResults(ctx, req),
			WebFetchEnabled:         s.resolveWebFetchEnabled(req),
			WebFetchTopN:            s.resolveWebFetchTopN(req),
			TenantID:                retrievalTenantID,
			Images:                  req.ImageURLs,
			VLMModelID:              vlmModelID,
			ChatModelSupportsVision: chatModelSupportsVision,
			Attachments:             req.Attachments,
			Language:                types.LanguageNameFromContext(ctx),
			ComplexityRouting:       types.DefaultComplexityRoutingConfig(),
		},
		PipelineState: types.PipelineState{
			RewriteQuery:     req.Query,
			ImageDescription: req.ImageDescription,
			QuotedContext:    req.QuotedContext,
		},
		PipelineContext: types.PipelineContext{
			EventBus:      eventBus.AsEventBusInterface(),
			MessageID:     req.AssistantMessageID,
			UserMessageID: req.UserMessageID,
		},
	}
	chatManage.VerifiedRetrieve = func(retrieveCtx context.Context, query string) ([]*types.SearchResult, error) {
		return s.SearchKnowledge(retrieveCtx, chatManage.KnowledgeBaseIDs, chatManage.KnowledgeIDs, req.FilterDisabledFolders, query)
	}

	// Apply custom agent overrides (system prompt, temperature, retrieval params,
	// rewrite, fallback, FAQ strategy, history turns)
	s.applyAgentOverridesToChatManage(ctx, req.CustomAgent, chatManage)
	s.applyPlatformVerificationDefaults(ctx, &chatManage.VerifiedAnswer)

	// Determine pipeline based on knowledge bases availability and web search setting
	hasKB := len(knowledgeBaseIDs) > 0 || len(knowledgeIDs) > 0
	needsRAG := hasKB || req.WebSearchEnabled
	hasHistory := chatManage.MaxRounds > 0

	var pipeline []types.EventType
	if !needsRAG {
		// Pure chat — no retrieval needed.
		userContent := req.Query
		if req.ImageDescription != "" && !chatModelSupportsVision {
			userContent += "\n\n[用户上传图片内容]\n" + req.ImageDescription
		}
		if req.QuotedContext != "" {
			userContent += "\n\n" + req.QuotedContext
		}
		// Inject attachment content for pure-chat path (RAG path handles this in INTO_CHAT_MESSAGE).
		if len(req.Attachments) > 0 {
			userContent += req.Attachments.BuildPrompt()
		}
		chatManage.UserContent = userContent

		pipeline = types.NewPipelineBuilder().
			AddIf(hasHistory, types.LOAD_HISTORY).
			AddIf(chatManage.EnableMemory, types.MEMORY_RETRIEVAL).
			Add(types.CHAT_COMPLETION_STREAM).
			AddIf(chatManage.EnableMemory, types.MEMORY_STORAGE).
			Build()
	} else {
		// RAG — dynamically assemble based on feature flags.
		pipeline = types.NewPipelineBuilder().
			Add(types.LOAD_HISTORY).
			Add(types.QUERY_UNDERSTAND).
			Add(types.CHUNK_SEARCH_PARALLEL).
			Add(types.CHUNK_RERANK).
			AddIf(req.WebSearchEnabled, types.WEB_FETCH).
			Add(types.CHUNK_MERGE).
			Add(types.FILTER_TOP_K).
			Add(types.DATA_ANALYSIS).
			Add(types.INTO_CHAT_MESSAGE).
			Add(types.CHAT_COMPLETION_STREAM).
			Build()
	}

	logger.Infof(ctx, "Assembled pipeline (%d stages), hasKB=%v, webSearch=%v, history=%v",
		len(pipeline), hasKB, req.WebSearchEnabled, hasHistory)

	// Start knowledge QA event processing (set session tenant so pipeline session/message lookups use session owner)
	ctx = context.WithValue(ctx, types.SessionTenantIDContextKey, req.Session.TenantID)
	logger.Info(ctx, "Triggering question answering event")
	err = s.KnowledgeQAByEvent(ctx, chatManage, pipeline)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"session_id": req.Session.ID,
		})
		return err
	}

	// Emit the references/telemetry event even when retrieval is empty so a
	// formal acceptance run can distinguish graph skip from a missing stream
	// observation on refusal and unanswerable cases.
	{
		logger.Infof(ctx, "Emitting references event with %d results", len(chatManage.MergeResult))
		skip := chatpipeline.AssessGraphSkip(chatManage)
		graphRequested := skip.Layer1Allowed
		graphUsed := chatManage.GraphSearchResult != nil && (len(chatManage.GraphSearchResult.Paths) > 0 || len(chatManage.GraphSearchResult.Citations) > 0)
		graphReason, graphReasonLegacy := chatpipeline.ResolveGraphTelemetryReason(graphRequested, graphUsed, skip)
		pathSummaries := chatpipeline.SummarizeGraphPaths(chatManage.GraphSearchResult)
		hasVerification := chatManage.VerifiedResult != nil
		confidence := 0.0
		if hasVerification {
			confidence = chatManage.VerifiedResult.Confidence
		}
		pathSummaries = chatpipeline.RankGraphPathsForDisplay(pathSummaries, confidence, hasVerification)
		graphTelemetry := map[string]interface{}{
			"requested":     graphRequested,
			"used":          graphUsed,
			"reason":        graphReason,
			"reason_legacy": graphReasonLegacy,
		}
		if len(pathSummaries) > 0 {
			graphTelemetry["paths"] = pathSummaries
		}
		telemetry := map[string]interface{}{
			"routing":           routingDecisionSummary(chatManage.RoutingDecision),
			"graph":             graphTelemetry,
			"verification_path": verifiedExecutionPath(chatManage.VerifiedResult),
		}
		if err := eventBus.Emit(ctx, event.Event{
			ID:        generateEventID("references"),
			Type:      event.EventAgentReferences,
			SessionID: req.Session.ID,
			Data: event.AgentReferencesData{
				References: chatManage.MergeResult,
				Extra:      telemetry,
			},
		}); err != nil {
			logger.Errorf(ctx, "Failed to emit references event: %v", err)
		}
	}

	// Note: Answer events are now emitted directly by chat_completion_stream plugin
	// Completion event will be emitted when the last answer event has Done=true
	// We can optionally add a completion watcher here if needed, but for now
	// the frontend can detect completion from the Done flag

	logger.Info(ctx, "Knowledge base question answering initiated")
	return nil
}

func verifiedExecutionPath(answer *types.VerifiedAnswer) string {
	if answer == nil {
		return ""
	}
	return answer.ExecutionPath
}

// resolveKnowledgeBasesFromAgent resolves knowledge base IDs based on agent's KBSelectionMode.
// sessionTenantID is the tenant of the current session (caller); it is compared with
// customAgent.TenantID to detect the shared-agent scenario and avoid leaking the
// current user's personal shared KBs into the agent's retrieval scope.
//
// Returns the resolved knowledge base IDs based on the selection mode:
//   - "all": fetches all knowledge bases for the tenant
//   - "selected": uses the explicitly configured knowledge bases
//   - "none": returns empty slice
//   - default: falls back to configured knowledge bases for backward compatibility
func (s *sessionService) resolveKnowledgeBasesFromAgent(
	ctx context.Context,
	customAgent *types.CustomAgent,
	sessionTenantID uint64,
) []string {
	if customAgent == nil {
		return nil
	}

	switch customAgent.Config.KBSelectionMode {
	case "all":
		// Authoritative capability filter for the runtime path. The frontend
		// editor and @mention dropdown apply the same filter, but we don't
		// trust the client here: a stale session payload or API caller could
		// still ask us to retrieve against an incompatible KB and we'd rather
		// just drop it (and log) than feed it to tools that would no-op.
		capFilter := tools.DeriveKBFilterFromTools(customAgent.Config.AllowedTools)
		accept := func(kb *types.KnowledgeBase) bool {
			if kb == nil {
				return false
			}
			if capFilter.IsEmpty() {
				return true
			}
			return tools.KBSatisfiesToolRequirements(kb.Capabilities(), customAgent.Config.AllowedTools)
		}

		// Get own knowledge bases (uses ctx TenantID = agent's tenant)
		allKBs, err := s.knowledgeBaseService.ListKnowledgeBases(ctx)
		if err != nil {
			logger.Warnf(ctx, "Failed to list all knowledge bases: %v", err)
		}
		kbIDSet := make(map[string]bool)
		kbIDs := make([]string, 0, len(allKBs))
		ownSkipped := 0
		for _, kb := range allKBs {
			if !accept(kb) {
				ownSkipped++
				continue
			}
			kbIDs = append(kbIDs, kb.ID)
			kbIDSet[kb.ID] = true
		}

		// For shared agents (session tenant != agent tenant), only use the agent
		// tenant's own KBs. Including the current user's shared KBs would leak
		// unrelated KBs from other organisations into the agent's retrieval scope.
		isSharedAgent := sessionTenantID != 0 && sessionTenantID != customAgent.TenantID
		sharedSkipped := 0
		if !isSharedAgent {
			tenantID := types.MustTenantIDFromContext(ctx)
			userIDVal := ctx.Value(types.UserIDContextKey)
			if userIDVal != nil {
				if userID, ok := userIDVal.(string); ok && userID != "" && s.kbShareService != nil {
					sharedList, err := s.kbShareService.ListSharedKnowledgeBases(ctx, userID, tenantID)
					if err != nil {
						logger.Warnf(ctx, "Failed to list shared knowledge bases: %v", err)
					} else {
						for _, info := range sharedList {
							if info == nil || info.KnowledgeBase == nil || kbIDSet[info.KnowledgeBase.ID] {
								continue
							}
							if !accept(info.KnowledgeBase) {
								sharedSkipped++
								continue
							}
							kbIDs = append(kbIDs, info.KnowledgeBase.ID)
							kbIDSet[info.KnowledgeBase.ID] = true
						}
					}
				}
			}
		} else {
			logger.Infof(ctx, "Shared agent detected (session tenant %d != agent tenant %d): skipping user's shared KBs",
				sessionTenantID, customAgent.TenantID)
		}

		if ownSkipped+sharedSkipped > 0 {
			logger.Infof(ctx,
				"KBSelectionMode=all: tool-capability filter removed %d own + %d shared KBs (agent=%s, tools=%v)",
				ownSkipped, sharedSkipped, customAgent.ID, customAgent.Config.AllowedTools)
		}
		logger.Infof(ctx, "KBSelectionMode=all: loaded %d knowledge bases (own + shared)", len(kbIDs))
		return kbIDs
	case "selected":
		logger.Infof(ctx, "KBSelectionMode=selected: using %d configured knowledge bases", len(customAgent.Config.KnowledgeBases))
		return customAgent.Config.KnowledgeBases
	case "none":
		logger.Infof(ctx, "KBSelectionMode=none: no knowledge bases configured")
		return nil
	default:
		// Default to "selected" behavior for backward compatibility
		if len(customAgent.Config.KnowledgeBases) > 0 {
			logger.Infof(ctx, "KBSelectionMode not set: using %d configured knowledge bases", len(customAgent.Config.KnowledgeBases))
		}
		return customAgent.Config.KnowledgeBases
	}
}

// buildSearchTargets computes the unified search targets from knowledgeBaseIDs and knowledgeIDs.
// tenantID is the retrieval scope: session.TenantID or effective tenant from shared agent (set by handler).
// This is called once at the request entry point to avoid repeated queries later in the pipeline.
// Logic:
//   - Without knowledgeIDs, each knowledgeBaseID becomes a full-KB target.
//   - With knowledgeIDs, every document must belong to the supplied KB scope and
//     only those documents become targets.
func (s *sessionService) buildSearchTargets(
	ctx context.Context,
	tenantID uint64,
	knowledgeBaseIDs []string,
	knowledgeIDs []string,
	filterDisabledFolders bool,
	explicitTagIDs ...[]string,
) (types.SearchTargets, error) {
	var targets types.SearchTargets
	type searchableTagProvider interface {
		SearchableTagIDs(context.Context, uint64, string) ([]string, error)
	}
	type integrationFolderScopeProvider interface {
		IntegrationFolderIDsForKnowledgeBase(context.Context, uint64, string, []string) ([]string, error)
	}
	searchableTags := func(scopeTenantID uint64, kbID string) ([]string, error) {
		var tagIDs []string
		if len(explicitTagIDs) > 0 {
			provider, ok := s.knowledgeService.(integrationFolderScopeProvider)
			if !ok {
				return nil, errors.New("integration folder scope is unavailable")
			}
			var err error
			tagIDs, err = provider.IntegrationFolderIDsForKnowledgeBase(ctx, scopeTenantID, kbID, explicitTagIDs[0])
			if err != nil || !filterDisabledFolders {
				return tagIDs, err
			}
		}
		if !filterDisabledFolders {
			return nil, nil
		}
		provider, ok := s.knowledgeService.(searchableTagProvider)
		if !ok {
			return nil, nil
		}
		searchable, err := provider.SearchableTagIDs(ctx, scopeTenantID, kbID)
		if err != nil || len(explicitTagIDs) == 0 {
			return searchable, err
		}
		allowed := make(map[string]struct{}, len(searchable))
		for _, id := range searchable {
			allowed[id] = struct{}{}
		}
		filtered := make([]string, 0, len(tagIDs))
		for _, id := range tagIDs {
			if _, ok := allowed[id]; ok {
				filtered = append(filtered, id)
			}
		}
		return filtered, nil
	}

	// Build a map from KB ID to TenantID for all KBs we need to process
	kbTenantMap := make(map[string]uint64)

	allowedKBSet := make(map[string]struct{}, len(knowledgeBaseIDs))

	// First pass: batch-fetch KBs, then resolve tenant per ID (tenant scope already set by caller)
	if len(knowledgeBaseIDs) > 0 {
		kbs, _ := s.knowledgeBaseService.GetKnowledgeBasesByIDsOnly(ctx, knowledgeBaseIDs)
		kbByID := make(map[string]*types.KnowledgeBase, len(kbs))
		for _, kb := range kbs {
			if kb != nil {
				kbByID[kb.ID] = kb
			}
		}
		userID, _ := types.UserIDFromContext(ctx)
		for _, kbID := range knowledgeBaseIDs {
			kb := kbByID[kbID]
			if kb == nil {
				return nil, werrors.NewForbiddenError("Knowledge base is outside the authorized scope")
			}
			allowedKBSet[kbID] = struct{}{}
			if kb.TenantID == tenantID {
				kbTenantMap[kbID] = tenantID
			} else if s.kbShareService != nil && userID != "" {
				hasAccess, _ := s.kbShareService.HasKBPermission(ctx, kbID, userID, types.OrgRoleViewer)
				if hasAccess {
					kbTenantMap[kbID] = kb.TenantID
				} else {
					kbTenantMap[kbID] = tenantID
				}
			} else {
				kbTenantMap[kbID] = tenantID
			}
			if len(knowledgeIDs) == 0 {
				tagIDs, tagErr := searchableTags(kbTenantMap[kbID], kbID)
				if tagErr != nil {
					return nil, tagErr
				}
				if tagIDs != nil && len(tagIDs) == 0 {
					continue
				}
				targets = append(targets, &types.SearchTarget{
					Type:            types.SearchTargetTypeKnowledgeBase,
					KnowledgeBaseID: kbID,
					TenantID:        kbTenantMap[kbID],
					TagIDs:          tagIDs,
				})
			}
		}
	}

	// Process individual knowledge IDs (include shared KB files the user has access to)
	if len(knowledgeIDs) > 0 {
		knowledgeList, err := s.knowledgeService.GetKnowledgeBatchWithSharedAccess(ctx, tenantID, knowledgeIDs)
		if err != nil {
			logger.Warnf(ctx, "Failed to get knowledge batch for search targets: %v", err)
			return nil, err
		}

		requestedKnowledgeIDs := make(map[string]struct{}, len(knowledgeIDs))
		for _, knowledgeID := range knowledgeIDs {
			requestedKnowledgeIDs[knowledgeID] = struct{}{}
		}
		if len(knowledgeList) != len(requestedKnowledgeIDs) {
			return nil, werrors.NewForbiddenError("Knowledge is outside the authorized scope")
		}

		// Group the explicitly requested documents by KB after enforcing the
		// caller-supplied KB boundary.
		kbToKnowledgeIDs := make(map[string][]string)
		resolvedKnowledgeIDs := make(map[string]struct{}, len(knowledgeList))
		for _, k := range knowledgeList {
			if k == nil || k.KnowledgeBaseID == "" {
				return nil, werrors.NewForbiddenError("Knowledge is outside the authorized scope")
			}
			if _, requested := requestedKnowledgeIDs[k.ID]; !requested {
				return nil, werrors.NewForbiddenError("Knowledge is outside the authorized scope")
			}
			if _, duplicate := resolvedKnowledgeIDs[k.ID]; duplicate {
				return nil, werrors.NewForbiddenError("Knowledge is outside the authorized scope")
			}
			resolvedKnowledgeIDs[k.ID] = struct{}{}
			if len(allowedKBSet) > 0 {
				if _, allowed := allowedKBSet[k.KnowledgeBaseID]; !allowed {
					return nil, werrors.NewForbiddenError("Knowledge is outside the authorized knowledge base scope")
				}
			}
			if kbTenantMap[k.KnowledgeBaseID] == 0 {
				kbTenantMap[k.KnowledgeBaseID] = k.TenantID
			}
			tagIDs, tagErr := searchableTags(k.TenantID, k.KnowledgeBaseID)
			if tagErr != nil {
				return nil, tagErr
			}
			if tagIDs != nil {
				allowed := false
				for _, tagID := range tagIDs {
					if tagID == k.TagID {
						allowed = true
						break
					}
				}
				if !allowed {
					if len(explicitTagIDs) > 0 {
						return nil, errors.New("invalid_knowledge_folder_scope")
					}
					logger.Infof(ctx, "Skipping knowledge %s because its folder is disabled for search", k.ID)
					continue
				}
			}
			kbToKnowledgeIDs[k.KnowledgeBaseID] = append(kbToKnowledgeIDs[k.KnowledgeBaseID], k.ID)
		}
		if len(resolvedKnowledgeIDs) != len(requestedKnowledgeIDs) {
			return nil, werrors.NewForbiddenError("Knowledge is outside the authorized scope")
		}

		// Create SearchTargetTypeKnowledge targets for each KB with specific files
		for kbID, kidList := range kbToKnowledgeIDs {
			kbTenant := kbTenantMap[kbID]
			if kbTenant == 0 {
				kbTenant = tenantID // fallback
			}
			targets = append(targets, &types.SearchTarget{
				Type:            types.SearchTargetTypeKnowledge,
				KnowledgeBaseID: kbID,
				TenantID:        kbTenant,
				KnowledgeIDs:    kidList,
			})
		}
	}

	logger.Infof(ctx, "Built %d search targets from %d KB IDs and %d knowledge IDs, kbTenantMap=%v",
		len(targets), len(knowledgeBaseIDs), len(knowledgeIDs), kbTenantMap)

	return targets, nil
}

// KnowledgeQAByEvent processes knowledge QA through a series of events in the pipeline
func (s *sessionService) KnowledgeQAByEvent(ctx context.Context,
	chatManage *types.ChatManage, eventList []types.EventType,
) error {
	logger.Info(ctx, "Start processing knowledge base question answering through events")
	logger.Infof(ctx, "Knowledge base question answering parameters, session ID: %s, query: %s",
		chatManage.SessionID, chatManage.Query)

	methods := make([]string, len(eventList))
	for i, event := range eventList {
		methods[i] = string(event)
	}
	logger.Infof(ctx, "Trigger event list: %v", methods)

	pipelineStart := time.Now()
	for _, eventType := range eventList {
		stageStart := time.Now()
		err := s.eventManager.Trigger(ctx, eventType, chatManage)
		stageDuration := time.Since(stageStart)

		if err == chatpipeline.ErrSearchNothing {
			common.PipelineWarn(ctx, "Pipeline", "stage_fallback", map[string]interface{}{
				"event":       string(eventType),
				"duration_ms": stageDuration.Milliseconds(),
				"reason":      "search_nothing",
				"strategy":    string(chatManage.FallbackStrategy),
			})
			s.handleFallbackResponse(ctx, chatManage)
			return nil
		}

		if err != nil {
			common.PipelineError(ctx, "Pipeline", "stage_failed", map[string]interface{}{
				"event":       string(eventType),
				"duration_ms": stageDuration.Milliseconds(),
				"error_type":  err.ErrorType,
				"description": err.Description,
			})
			return err.Err
		}

		common.PipelineInfo(ctx, "Pipeline", "stage_complete", map[string]interface{}{
			"event":       string(eventType),
			"duration_ms": stageDuration.Milliseconds(),
		})
	}

	common.PipelineInfo(ctx, "Pipeline", "all_stages_complete", map[string]interface{}{
		"session_id":        chatManage.SessionID,
		"total_stages":      len(eventList),
		"total_duration_ms": time.Since(pipelineStart).Milliseconds(),
	})
	return nil
}

// SearchKnowledge performs knowledge base search without LLM summarization
// knowledgeBaseIDs: list of knowledge base IDs to search (supports multi-KB)
// knowledgeIDs: list of specific knowledge (file) IDs to search
func (s *sessionService) SearchKnowledge(ctx context.Context,
	knowledgeBaseIDs []string, knowledgeIDs []string, filterDisabledFolders bool, query string,
) ([]*types.SearchResult, error) {
	return s.searchKnowledge(ctx, knowledgeBaseIDs, knowledgeIDs, nil, filterDisabledFolders, query)
}

func (s *sessionService) SearchKnowledgeWithFolders(ctx context.Context,
	knowledgeBaseIDs []string, knowledgeIDs []string, folderIDs []string, filterDisabledFolders bool, query string,
) ([]*types.SearchResult, error) {
	return s.searchKnowledge(ctx, knowledgeBaseIDs, knowledgeIDs, folderIDs, filterDisabledFolders, query)
}

func (s *sessionService) searchKnowledge(ctx context.Context,
	knowledgeBaseIDs []string, knowledgeIDs []string, folderIDs []string, filterDisabledFolders bool, query string,
) ([]*types.SearchResult, error) {
	logger.Info(ctx, "Start knowledge base search without LLM summary")
	logger.Infof(ctx, "Knowledge base search parameters, knowledge base IDs: %v, knowledge IDs: %v, query: %s",
		knowledgeBaseIDs, knowledgeIDs, query)

	// Get tenant ID from context
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok {
		logger.Error(ctx, "Failed to get tenant ID from context")
		return nil, fmt.Errorf("tenant ID not found in context")
	}

	// Build unified search targets (computed once, used throughout pipeline)
	var searchTargets types.SearchTargets
	var err error
	if folderIDs == nil {
		searchTargets, err = s.buildSearchTargets(ctx, tenantID, knowledgeBaseIDs, knowledgeIDs, filterDisabledFolders)
	} else {
		searchTargets, err = s.buildSearchTargets(ctx, tenantID, knowledgeBaseIDs, knowledgeIDs, filterDisabledFolders, folderIDs)
	}
	if err != nil {
		logger.Warnf(ctx, "Failed to build search targets: %v", err)
		return nil, err
	}

	if len(searchTargets) == 0 {
		logger.Warn(ctx, "No search targets available, returning empty results")
		return []*types.SearchResult{}, nil
	}

	// Create default retrieval parameters — prefer tenant RetrievalConfig, fallback to built-in defaults
	userID, _ := types.UserIDFromContext(ctx)

	rc := s.effectiveRetrievalConfig(ctx, tenantID)

	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			Query:               query,
			UserID:              userID,
			TenantID:            tenantID,
			KnowledgeBaseIDs:    knowledgeBaseIDs,
			KnowledgeIDs:        knowledgeIDs,
			SearchTargets:       searchTargets,
			MaxRounds:           s.cfg.Conversation.MaxRounds,
			EmbeddingTopK:       rc.EmbeddingTopK,
			VectorRecallTopK:    rc.VectorRecallTopK,
			KeywordRecallTopK:   rc.KeywordRecallTopK,
			RRFVectorWeight:     rc.RRFVectorWeight,
			VectorThreshold:     rc.VectorThreshold,
			KeywordThreshold:    rc.KeywordThreshold,
			RerankCandidateTopK: rc.RerankCandidateTopK,
			RerankTopK:          rc.RerankTopK,
			RerankThreshold:     rc.RerankThreshold,
		},
		PipelineState: types.PipelineState{
			RewriteQuery: query,
		},
	}

	// Retrieval always uses the platform-managed default rerank model.
	if model, defaultErr := s.modelService.GetDefaultModel(ctx, types.ModelTypeRerank, "rerank"); defaultErr == nil {
		chatManage.RerankModelID = model.ID
	}

	// Use specific event list, only including retrieval-related events, not LLM summarization
	searchEvents := []types.EventType{
		types.CHUNK_SEARCH, // Vector search
		types.CHUNK_RERANK, // Rerank search results
		types.CHUNK_MERGE,  // Merge search results
		types.FILTER_TOP_K, // Filter top K results
	}

	logger.Infof(ctx, "Trigger search event list: %v", searchEvents)

	for _, event := range searchEvents {
		logger.Infof(ctx, "Starting to trigger search event: %v", event)
		err := s.eventManager.Trigger(ctx, event, chatManage)

		if err == chatpipeline.ErrSearchNothing {
			logger.Warnf(ctx, "Event %v triggered, search result is empty", event)
			return []*types.SearchResult{}, nil
		}

		if err != nil {
			logger.Errorf(ctx, "Event triggering failed, event: %v, error type: %s, description: %s, error: %v",
				event, err.ErrorType, err.Description, err.Err)
			return nil, err.Err
		}
		logger.Infof(ctx, "Event %v triggered successfully", event)
	}

	logger.Infof(ctx, "Knowledge base search completed, found %d results", len(chatManage.MergeResult))
	return chatManage.MergeResult, nil
}

// handleFallbackResponse handles fallback response based on strategy
func (s *sessionService) handleFallbackResponse(ctx context.Context, chatManage *types.ChatManage) {
	if chatManage.FallbackStrategy == types.FallbackStrategyModel {
		s.handleModelFallback(ctx, chatManage)
	} else {
		s.handleFixedFallback(ctx, chatManage)
	}
}

// handleFixedFallback handles fixed fallback response
func (s *sessionService) handleFixedFallback(ctx context.Context, chatManage *types.ChatManage) {
	fallbackContent := chatManage.FallbackResponse
	chatManage.ChatResponse = &types.ChatResponse{Content: fallbackContent}
	s.emitFallbackAnswer(ctx, chatManage, fallbackContent)
}

// handleModelFallback handles model-based fallback response using streaming
func (s *sessionService) handleModelFallback(ctx context.Context, chatManage *types.ChatManage) {
	// Check if FallbackPrompt is available
	if chatManage.FallbackPrompt == "" {
		logger.Warnf(ctx, "Fallback strategy is 'model' but FallbackPrompt is empty, falling back to fixed response")
		s.handleFixedFallback(ctx, chatManage)
		return
	}

	// Render template with Query variable
	promptContent, err := s.renderFallbackPrompt(ctx, chatManage)
	if err != nil {
		logger.Errorf(ctx, "Failed to render fallback prompt: %v, falling back to fixed response", err)
		s.handleFixedFallback(ctx, chatManage)
		return
	}

	// Check if EventBus is available for streaming
	if chatManage.EventBus == nil {
		logger.Warnf(ctx, "EventBus not available for streaming fallback, falling back to fixed response")
		s.handleFixedFallback(ctx, chatManage)
		return
	}

	// Get chat model
	chatModel, err := s.modelService.GetChatModel(ctx, chatManage.ChatModelID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get chat model for fallback: %v, falling back to fixed response", err)
		s.handleFixedFallback(ctx, chatManage)
		return
	}

	// Prepare chat options
	thinking := false
	opt := &chat.ChatOptions{
		Temperature:         chatManage.SummaryConfig.Temperature,
		MaxCompletionTokens: chatManage.SummaryConfig.MaxCompletionTokens,
		Thinking:            &thinking,
	}

	// Start streaming response
	userMsg := chat.Message{Role: "user", Content: promptContent}
	if chatManage.ChatModelSupportsVision && len(chatManage.Images) > 0 {
		userMsg.Images = chatManage.Images
	}
	responseChan, err := chatModel.ChatStream(ctx, []chat.Message{userMsg}, opt)
	if err != nil {
		logger.Errorf(ctx, "Failed to start streaming fallback response: %v, falling back to fixed response", err)
		s.handleFixedFallback(ctx, chatManage)
		return
	}

	if responseChan == nil {
		logger.Errorf(ctx, "Chat stream returned nil channel, falling back to fixed response")
		s.handleFixedFallback(ctx, chatManage)
		return
	}

	// The caller persists the assistant message after this method returns, so the
	// fallback stream must finish before returning or the request context is
	// cancelled with an empty answer.
	s.consumeFallbackStream(ctx, chatManage, responseChan)
}

// renderFallbackPrompt renders the fallback prompt template with query and image context.
func (s *sessionService) renderFallbackPrompt(ctx context.Context, chatManage *types.ChatManage) (string, error) {
	query := chatManage.Query
	if rq := strings.TrimSpace(chatManage.RewriteQuery); rq != "" {
		query = rq
	}

	kbDocuments := s.buildKBDocumentListing(ctx, chatManage)

	result := types.RenderPromptPlaceholders(chatManage.FallbackPrompt, types.PlaceholderValues{
		"query":        query,
		"language":     chatManage.Language,
		"kb_documents": kbDocuments,
	})

	if chatManage.ImageDescription != "" && !chatManage.ChatModelSupportsVision {
		result += "\n\n[用户上传图片内容]\n" + chatManage.ImageDescription
	}
	if chatManage.QuotedContext != "" {
		result += "\n\n" + chatManage.QuotedContext
	}
	return result, nil
}

// buildKBDocumentListing returns a concise listing of documents in the knowledge bases
// associated with the current pipeline. This gives the LLM visibility into KB contents
// when vector/keyword search returns empty (e.g., broad browse queries).
func (s *sessionService) buildKBDocumentListing(ctx context.Context, chatManage *types.ChatManage) string {
	// Collect unique KB IDs from search targets
	kbIDs := make(map[string]struct{})
	for _, t := range chatManage.SearchTargets {
		kbIDs[t.KnowledgeBaseID] = struct{}{}
	}
	for _, id := range chatManage.KnowledgeBaseIDs {
		kbIDs[id] = struct{}{}
	}
	if len(kbIDs) == 0 {
		return ""
	}

	const maxDocuments = 50
	var b strings.Builder
	total := 0

	for kbID := range kbIDs {
		if total >= maxDocuments {
			break
		}
		knowledges, err := s.knowledgeService.ListKnowledgeByKnowledgeBaseID(ctx, kbID)
		if err != nil {
			logger.Warnf(ctx, "buildKBDocumentListing: failed to list knowledge for KB %s: %v", kbID, err)
			continue
		}
		for _, k := range knowledges {
			if total >= maxDocuments {
				break
			}
			if k.EnableStatus != "enabled" {
				continue
			}
			title := k.Title
			if title == "" {
				title = k.FileName
			}
			if title == "" {
				continue
			}
			fmt.Fprintf(&b, "- %s", title)
			if k.FileType != "" {
				fmt.Fprintf(&b, " (%s)", k.FileType)
			}
			if k.Description != "" {
				desc := k.Description
				if len([]rune(desc)) > 100 {
					desc = string([]rune(desc)[:100]) + "..."
				}
				fmt.Fprintf(&b, ": %s", desc)
			}
			b.WriteString("\n")
			total++
		}
	}

	if b.Len() == 0 {
		return ""
	}

	if total >= maxDocuments {
		fmt.Fprintf(&b, "... (showing first %d documents)\n", maxDocuments)
	}

	return b.String()
}

// consumeFallbackStream consumes the streaming response and emits events
func (s *sessionService) consumeFallbackStream(
	ctx context.Context,
	chatManage *types.ChatManage,
	responseChan <-chan types.StreamResponse,
) {
	fallbackID := generateEventID("fallback")
	eventBus := chatManage.EventBus
	var finalContent string
	streamCompleted := false

	for response := range responseChan {
		// Emit event for each answer chunk
		if response.ResponseType == types.ResponseTypeAnswer {
			finalContent += response.Content
			if err := eventBus.Emit(ctx, types.Event{
				ID:        fallbackID,
				Type:      types.EventType(event.EventAgentFinalAnswer),
				SessionID: chatManage.SessionID,
				Data: event.AgentFinalAnswerData{
					Content:    response.Content,
					Done:       response.Done,
					IsFallback: true,
				},
			}); err != nil {
				logger.Errorf(ctx, "Failed to emit fallback answer chunk event: %v", err)
			}

			// Update ChatResponse with final content when done
			if response.Done {
				chatManage.ChatResponse = &types.ChatResponse{Content: finalContent}
				streamCompleted = true
				logger.Infof(ctx, "Fallback streaming response completed")
				break
			}
		}
	}

	// If channel closed without Done=true, emit final event with fixed response
	if !streamCompleted {
		logger.Warnf(ctx, "Fallback stream closed without completion, emitting final event with fixed response")
		s.emitFallbackAnswer(ctx, chatManage, chatManage.FallbackResponse)
	}
}

// emitFallbackAnswer emits fallback answer event
func (s *sessionService) emitFallbackAnswer(ctx context.Context, chatManage *types.ChatManage, content string) {
	if chatManage.EventBus == nil {
		return
	}

	fallbackID := generateEventID("fallback")
	if err := chatManage.EventBus.Emit(ctx, types.Event{
		ID:        fallbackID,
		Type:      types.EventType(event.EventAgentFinalAnswer),
		SessionID: chatManage.SessionID,
		Data: event.AgentFinalAnswerData{
			Content:    content,
			Done:       true,
			IsFallback: true,
		},
	}); err != nil {
		logger.Errorf(ctx, "Failed to emit fallback answer event: %v", err)
	} else {
		logger.Infof(ctx, "Fallback answer event emitted successfully")
	}
}

// resolveWebSearchProviderID returns the web search provider ID to use for a pipeline request.
// Priority: agent config > tenant default (is_default=true)
func (s *sessionService) resolveWebSearchProviderID(ctx context.Context, req *types.QARequest, tenantID uint64) string {
	// 1. Agent-level override
	if req.CustomAgent != nil && req.CustomAgent.Config.WebSearchProviderID != "" {
		return req.CustomAgent.Config.WebSearchProviderID
	}
	// 2. Tenant default
	if s.webSearchProviderRepo != nil {
		if defaultProvider, err := s.webSearchProviderRepo.GetDefault(ctx, tenantID); err == nil && defaultProvider != nil {
			return defaultProvider.ID
		}
	}
	return ""
}

// resolveWebFetchEnabled returns whether auto web fetch is enabled for this request.
func (s *sessionService) resolveWebFetchEnabled(req *types.QARequest) bool {
	if req.CustomAgent != nil {
		return req.CustomAgent.Config.WebFetchEnabled
	}
	return false
}

// resolveWebFetchTopN returns how many pages to fetch after rerank.
func (s *sessionService) resolveWebFetchTopN(req *types.QARequest) int {
	if req.CustomAgent != nil && req.CustomAgent.Config.WebFetchTopN > 0 {
		return req.CustomAgent.Config.WebFetchTopN
	}
	return 3
}

// resolveWebSearchMaxResults returns the max results for web search.
// Priority: agent config > tenant default > default (10)
func (s *sessionService) resolveWebSearchMaxResults(ctx context.Context, req *types.QARequest) int {
	if req.CustomAgent != nil && req.CustomAgent.Config.WebSearchMaxResults > 0 {
		return req.CustomAgent.Config.WebSearchMaxResults
	}
	tenantInfo, _ := types.TenantInfoFromContext(ctx)
	if tenantInfo != nil && tenantInfo.WebSearchConfig != nil && tenantInfo.WebSearchConfig.MaxResults > 0 {
		return tenantInfo.WebSearchConfig.MaxResults
	}
	return 10
}
