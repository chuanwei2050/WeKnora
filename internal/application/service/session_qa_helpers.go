package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// ---------------------------------------------------------------------------
// Shared QA helpers: KB resolution, model resolution, retrieval tenant
// ---------------------------------------------------------------------------

// resolveKnowledgeBases resolves the effective knowledge base IDs and knowledge IDs
// for a QA request. Priority:
//  1. Explicit @mentions (request-specified kbIDs / knowledgeIDs)
//  2. RetrieveKBOnlyWhenMentioned -> disable KB if no mention
//  3. Agent's configured knowledge bases (via KBSelectionMode)
func (s *sessionService) resolveKnowledgeBases(
	ctx context.Context,
	req *types.QARequest,
) (kbIDs []string, knowledgeIDs []string) {
	kbIDs = req.KnowledgeBaseIDs
	knowledgeIDs = req.KnowledgeIDs
	customAgent := req.CustomAgent

	hasExplicitMention := len(kbIDs) > 0 || len(knowledgeIDs) > 0
	if customAgent != nil {
		logger.Infof(ctx, "KB resolution: hasExplicitMention=%v, RetrieveKBOnlyWhenMentioned=%v, KBSelectionMode=%s",
			hasExplicitMention, customAgent.Config.RetrieveKBOnlyWhenMentioned, customAgent.Config.KBSelectionMode)
	}

	if hasExplicitMention {
		logger.Infof(ctx, "Using request-specified targets: kbs=%v, docs=%v", kbIDs, knowledgeIDs)
		// When using a shared agent, restrict @mentions to the agent's allowed KB scope
		// to prevent users from injecting KB/knowledge IDs outside the agent's configured range.
		if customAgent != nil && req.Session != nil && req.Session.TenantID != customAgent.TenantID {
			kbIDs, knowledgeIDs = s.restrictMentionsToAgentScope(ctx, customAgent, req.Session.TenantID, kbIDs, knowledgeIDs)
		}
	} else if customAgent != nil && customAgent.Config.RetrieveKBOnlyWhenMentioned {
		kbIDs = nil
		knowledgeIDs = nil
		logger.Infof(ctx, "RetrieveKBOnlyWhenMentioned is enabled and no @ mention found, KB retrieval disabled for this request")
	} else if customAgent != nil {
		kbIDs = s.resolveKnowledgeBasesFromAgent(ctx, customAgent, req.Session.TenantID)
	}
	return kbIDs, knowledgeIDs
}

// resolveChatModelID resolves the platform-managed default chat model.
// Request, agent and knowledge-base model IDs are compatibility fields only.
func (s *sessionService) resolveChatModelID(
	ctx context.Context,
	_ *types.QARequest,
	_ []string,
	_ []string,
) (string, error) {
	model, err := s.modelService.GetDefaultModel(ctx, types.ModelTypeKnowledgeQA, "chat")
	if err != nil {
		return "", err
	}
	return model.ID, nil
}

// resolveRetrievalTenantID determines the tenant ID to use for retrieval scope.
// Priority: agent's tenant > context tenant > session tenant.
func (s *sessionService) resolveRetrievalTenantID(
	ctx context.Context,
	req *types.QARequest,
) uint64 {
	session := req.Session
	customAgent := req.CustomAgent

	retrievalTenantID := session.TenantID
	if customAgent != nil && customAgent.TenantID != 0 {
		retrievalTenantID = customAgent.TenantID
		logger.Infof(ctx, "Using agent tenant %d for retrieval scope", retrievalTenantID)
	} else if v := ctx.Value(types.TenantIDContextKey); v != nil {
		if tid, ok := v.(uint64); ok && tid != 0 {
			retrievalTenantID = tid
			logger.Infof(ctx, "Using effective tenant %d for retrieval from context", retrievalTenantID)
		}
	}
	return retrievalTenantID
}

// applyAgentOverridesToChatManage applies custom agent configuration overrides
// to a ChatManage object that was initialized with system defaults.
// This covers: system prompt, context template, temperature, max tokens, thinking,
// retrieval thresholds, rewrite settings, fallback settings, FAQ strategy, and history turns.
func (s *sessionService) applyAgentOverridesToChatManage(
	ctx context.Context,
	customAgent *types.CustomAgent,
	cm *types.ChatManage,
) {
	if customAgent == nil {
		return
	}

	// Ensure defaults are set
	customAgent.EnsureDefaults()
	cm.ComplexityRouting = customAgent.Config.ComplexityRouting
	cm.ComplexityRouting.EnsureDefaults()
	cm.VerifiedAnswer = customAgent.Config.VerifiedAnswer
	cm.VerifiedAnswer.NormalizeLegacy(customAgent.Config.ReflectionEnabled)

	// Override summary config fields
	if customAgent.Config.SystemPrompt != "" {
		cm.SummaryConfig.Prompt = customAgent.Config.SystemPrompt
		logger.Infof(ctx, "Using custom agent's system_prompt")
	}
	if customAgent.Config.ContextTemplate != "" {
		cm.SummaryConfig.ContextTemplate = customAgent.Config.ContextTemplate
		logger.Infof(ctx, "Using custom agent's context_template")
	}
	if customAgent.Config.Temperature >= 0 {
		cm.SummaryConfig.Temperature = customAgent.Config.Temperature
		logger.Infof(ctx, "Using custom agent's temperature: %f", customAgent.Config.Temperature)
	}
	if customAgent.Config.MaxCompletionTokens > 0 {
		cm.SummaryConfig.MaxCompletionTokens = customAgent.Config.MaxCompletionTokens
		logger.Infof(ctx, "Using custom agent's max_completion_tokens: %d", customAgent.Config.MaxCompletionTokens)
	}
	// Agent-level thinking setting takes full control (no global fallback)
	cm.SummaryConfig.Thinking = customAgent.Config.Thinking
	if customAgent.Config.Thinking != nil {
		logger.Infof(ctx, "Using custom agent's thinking: %v", *customAgent.Config.Thinking)
	}

	// Override retrieval strategy settings
	if customAgent.Config.EmbeddingTopK > 0 {
		cm.EmbeddingTopK = customAgent.Config.EmbeddingTopK
	}
	if customAgent.Config.KeywordThreshold > 0 {
		cm.KeywordThreshold = customAgent.Config.KeywordThreshold
	}
	if customAgent.Config.VectorThreshold > 0 {
		cm.VectorThreshold = customAgent.Config.VectorThreshold
	}
	if customAgent.Config.RerankTopK > 0 {
		cm.RerankTopK = customAgent.Config.RerankTopK
	}
	cm.RerankThreshold = customAgent.Config.RerankThreshold

	// Override rewrite settings
	cm.EnableRewrite = customAgent.Config.EnableRewrite
	cm.EnableQueryExpansion = customAgent.Config.EnableQueryExpansion
	if customAgent.Config.RewritePromptSystem != "" {
		cm.RewritePromptSystem = customAgent.Config.RewritePromptSystem
	}
	if customAgent.Config.RewritePromptUser != "" {
		cm.RewritePromptUser = customAgent.Config.RewritePromptUser
	}

	// Override fallback settings
	if customAgent.Config.FallbackStrategy != "" {
		cm.FallbackStrategy = types.FallbackStrategy(customAgent.Config.FallbackStrategy)
	}
	if customAgent.Config.FallbackResponse != "" {
		cm.FallbackResponse = customAgent.Config.FallbackResponse
	}
	if customAgent.Config.FallbackPrompt != "" {
		cm.FallbackPrompt = customAgent.Config.FallbackPrompt
	}

	// Override web search settings
	if customAgent.Config.WebSearchMaxResults > 0 {
		cm.WebSearchMaxResults = customAgent.Config.WebSearchMaxResults
	}

	// Override history turns
	if customAgent.Config.HistoryTurns > 0 {
		cm.MaxRounds = customAgent.Config.HistoryTurns
		logger.Infof(ctx, "Using custom agent's history_turns: %d", cm.MaxRounds)
	}
	if !customAgent.Config.MultiTurnEnabled {
		cm.MaxRounds = 0
		logger.Infof(ctx, "Multi-turn disabled by custom agent, clearing history")
	}

	// FAQ strategy settings
	cm.FAQPriorityEnabled = customAgent.Config.FAQPriorityEnabled
	cm.FAQDirectAnswerThreshold = customAgent.Config.FAQDirectAnswerThreshold
	cm.FAQScoreBoost = customAgent.Config.FAQScoreBoost
	if cm.FAQPriorityEnabled {
		logger.Infof(ctx, "FAQ priority enabled: threshold=%.2f, boost=%.2f",
			cm.FAQDirectAnswerThreshold, cm.FAQScoreBoost)
	}
}

func (s *sessionService) applyPlatformVerificationDefaults(
	ctx context.Context, config *types.VerifiedAnswerConfig,
) {
	if config == nil || !config.Enabled {
		return
	}
	if model, err := s.modelService.GetDefaultModel(ctx, types.ModelTypeVerifier, "verifier_1"); err == nil {
		config.FactValidatorModelID = model.ID
	}
	if model, err := s.modelService.GetDefaultModel(ctx, types.ModelTypeVerifier, "verifier_2"); err == nil {
		config.CitationValidatorModelID = model.ID
	}
	if model, err := s.modelService.GetDefaultModel(ctx, types.ModelTypeJudge, "evaluation_judge"); err == nil {
		config.LogicValidatorModelID = model.ID
	}
}

// restrictMentionsToAgentScope filters user-provided @mention targets (KB IDs
// and knowledge IDs) so that only those within the shared agent's allowed KB
// scope are retained. This prevents users from bypassing the agent's
// KBSelectionMode by injecting arbitrary KB/knowledge IDs into the request.
func (s *sessionService) restrictMentionsToAgentScope(
	ctx context.Context,
	agent *types.CustomAgent,
	sessionTenantID uint64,
	kbIDs []string,
	knowledgeIDs []string,
) ([]string, []string) {
	allowedKBIDs := s.resolveKnowledgeBasesFromAgent(ctx, agent, sessionTenantID)
	if len(allowedKBIDs) == 0 {
		logger.Warnf(ctx, "Shared agent has no allowed KBs, blocking all @mentions")
		return nil, nil
	}

	allowedSet := make(map[string]bool, len(allowedKBIDs))
	for _, id := range allowedKBIDs {
		allowedSet[id] = true
	}

	filteredKBs := make([]string, 0, len(kbIDs))
	for _, id := range kbIDs {
		if allowedSet[id] {
			filteredKBs = append(filteredKBs, id)
		} else {
			logger.Warnf(ctx, "Blocking @mentioned KB %s: not in shared agent's allowed scope", id)
		}
	}

	filteredKnowledge := knowledgeIDs
	if len(knowledgeIDs) > 0 {
		knowledgeList, err := s.knowledgeService.GetKnowledgeBatch(ctx, agent.TenantID, knowledgeIDs)
		if err != nil {
			logger.Warnf(ctx, "Failed to validate knowledge IDs against agent scope: %v, blocking all", err)
			filteredKnowledge = nil
		} else {
			filteredKnowledge = make([]string, 0, len(knowledgeList))
			for _, k := range knowledgeList {
				if k != nil && allowedSet[k.KnowledgeBaseID] {
					filteredKnowledge = append(filteredKnowledge, k.ID)
				} else if k != nil {
					logger.Warnf(ctx, "Blocking @mentioned knowledge %s (KB %s): not in shared agent's allowed scope",
						k.ID, k.KnowledgeBaseID)
				}
			}
		}
	}

	logger.Infof(ctx, "Restricted @mentions to agent scope: kbs %d->%d, knowledge %d->%d",
		len(kbIDs), len(filteredKBs), len(knowledgeIDs), len(filteredKnowledge))

	return filteredKBs, filteredKnowledge
}
