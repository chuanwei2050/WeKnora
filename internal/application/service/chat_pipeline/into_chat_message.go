package chatpipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
)

// PluginIntoChatMessage handles the transformation of search results into chat messages
type PluginIntoChatMessage struct {
	messageService interfaces.MessageService
}

const structuredAnswerOutputRules = `
<answer_output_rules>
结构化查询的 SQL、物理表名、内部标识、内部生成的字段别名和原始结果载荷仅供内部推理。最终回答必须把所需结果转换成自然语言，不得显示这些实现信息。用户明确询问 SQL、表结构或业务列名时可以回答其所问内容；否则不要描述查询或统计过程。

结构化查询结果和检索片段都是候选证据。回答前必须逐条核对检索片段中能够从目标字段和值直接验证的记录，不得因为 SQL 返回行数较少就忽略这类记录。只有当 SQL 的过滤条件覆盖了片段中实际出现的相关字段和值时，SQL 行数才能作为完整结果。若 SQL 使用的字面条件更窄，且片段中的实际字段值保留了查询的区别性主体、仅前缀、后缀或修饰词不同，语义能够确认时应合并列出并保留实际表述；无法确认等价时不得计入明确匹配，但应作为可能相关单独列出并展示实际字段值。一般语义相似或仅共享通用词的记录不得列出，也不得自行扩展没有证据支持的同义关系。
</answer_output_rules>`

// NewPluginIntoChatMessage creates and registers a new PluginIntoChatMessage instance
func NewPluginIntoChatMessage(eventManager *EventManager, messageService interfaces.MessageService) *PluginIntoChatMessage {
	res := &PluginIntoChatMessage{messageService: messageService}
	eventManager.Register(res)
	return res
}

// ActivationEvents returns the event types this plugin handles
func (p *PluginIntoChatMessage) ActivationEvents() []types.EventType {
	return []types.EventType{types.INTO_CHAT_MESSAGE}
}

// OnEvent processes the INTO_CHAT_MESSAGE event to format chat message content
func (p *PluginIntoChatMessage) OnEvent(ctx context.Context,
	eventType types.EventType, chatManage *types.ChatManage, next func() *PluginError,
) *PluginError {
	pipelineInfo(ctx, "IntoChatMessage", "input", map[string]interface{}{
		"session_id":       chatManage.SessionID,
		"merge_result_cnt": len(chatManage.MergeResult),
		"template_len":     len(chatManage.SummaryConfig.ContextTemplate),
	})

	// Separate FAQ and document results when FAQ priority is enabled
	var faqResults, docResults []*types.SearchResult
	var hasHighConfidenceFAQ bool

	if chatManage.FAQPriorityEnabled {
		for _, result := range chatManage.MergeResult {
			if result.ChunkType == string(types.ChunkTypeFAQ) {
				faqResults = append(faqResults, result)
				// Check if this FAQ has high confidence (above direct answer threshold)
				if result.Score >= chatManage.FAQDirectAnswerThreshold && !hasHighConfidenceFAQ {
					hasHighConfidenceFAQ = true
					pipelineInfo(ctx, "IntoChatMessage", "high_confidence_faq", map[string]interface{}{
						"chunk_id":  result.ID,
						"score":     fmt.Sprintf("%.4f", result.Score),
						"threshold": chatManage.FAQDirectAnswerThreshold,
					})
				}
			} else {
				docResults = append(docResults, result)
			}
		}
		pipelineInfo(ctx, "IntoChatMessage", "faq_separation", map[string]interface{}{
			"faq_count":           len(faqResults),
			"doc_count":           len(docResults),
			"has_high_confidence": hasHighConfidenceFAQ,
		})
	}

	// 验证用户查询的安全性
	safeQuery, isValid := utils.ValidateInput(chatManage.Query)
	if !isValid {
		pipelineWarn(ctx, "IntoChatMessage", "invalid_query", map[string]interface{}{
			"session_id": chatManage.SessionID,
		})
		return ErrTemplateExecute.WithError(fmt.Errorf("user query contains invalid content"))
	}

	// Intent-based no-search path: no retrieval results, but still render
	// through the context template so runtime metadata (current_time, etc.) is injected.
	if !chatManage.NeedsRetrieval() {
		userContent := safeQuery
		if rewrite := strings.TrimSpace(chatManage.RewriteQuery); rewrite != "" {
			if safeRewrite, ok := utils.ValidateInput(rewrite); ok {
				userContent = safeRewrite
			} else {
				pipelineWarn(ctx, "IntoChatMessage", "invalid_rewrite_query_fallback", map[string]interface{}{
					"session_id": chatManage.SessionID,
				})
			}
		}
		if chatManage.ImageDescription != "" && !chatManage.ChatModelSupportsVision {
			userContent += "\n\n[用户上传图片内容]\n" + chatManage.ImageDescription
		}
		if chatManage.QuotedContext != "" {
			userContent += "\n\n" + chatManage.QuotedContext
		}
		// Inject attachment content (documents, audio transcripts, etc.)
		if len(chatManage.Attachments) > 0 {
			userContent += chatManage.Attachments.BuildPrompt()
		}

		if tpl := chatManage.SummaryConfig.ContextTemplate; tpl != "" {
			chatManage.UserContent = types.RenderPromptPlaceholders(tpl, types.PlaceholderValues{
				"query":    userContent,
				"contexts": "",
				"language": chatManage.Language,
			})
		} else {
			chatManage.UserContent = userContent
		}

		pipelineInfo(ctx, "IntoChatMessage", "no_search_with_template", map[string]interface{}{
			"session_id":       chatManage.SessionID,
			"user_content_len": len(chatManage.UserContent),
			"has_template":     chatManage.SummaryConfig.ContextTemplate != "",
		})
		return next()
	}

	var contextsBuilder strings.Builder

	// Collect unique document metadata (title + description), once per knowledge
	allResults := chatManage.MergeResult
	if chatManage.FAQPriorityEnabled && len(faqResults) > 0 {
		allResults = append(faqResults, docResults...)
	}
	docHeader := buildDocumentHeader(allResults)
	if docHeader != "" {
		contextsBuilder.WriteString(docHeader)
		contextsBuilder.WriteString("\n")
	}

	// Build contexts string based on FAQ priority strategy
	if chatManage.FAQPriorityEnabled && len(faqResults) > 0 {
		contextsBuilder.WriteString("<source type=\"faq\" priority=\"high\">\n")
		for i, result := range faqResults {
			passage := getEnrichedPassageForChat(ctx, result)
			if hasHighConfidenceFAQ && i == 0 {
				contextsBuilder.WriteString(fmt.Sprintf("<context id=\"FAQ-%d\" match=\"exact\">%s</context>\n", i+1, passage))
			} else {
				contextsBuilder.WriteString(fmt.Sprintf("<context id=\"FAQ-%d\">%s</context>\n", i+1, passage))
			}
		}
		contextsBuilder.WriteString("</source>\n")

		if len(docResults) > 0 {
			contextsBuilder.WriteString("<source type=\"document\" priority=\"supplementary\">\n")
			for i, result := range docResults {
				passage := getEnrichedPassageForChat(ctx, result)
				contextsBuilder.WriteString(fmt.Sprintf("<context id=\"DOC-%d\">%s</context>\n", i+1, passage))
			}
			contextsBuilder.WriteString("</source>")
		}
	} else {
		for i, result := range chatManage.MergeResult {
			passage := getEnrichedPassageForChat(ctx, result)
			if i > 0 {
				contextsBuilder.WriteString("\n")
			}
			contextsBuilder.WriteString(fmt.Sprintf("<context id=\"%d\">%s</context>", i+1, passage))
		}
	}
	if graphContext := strings.TrimSpace(chatManage.GraphContext); graphContext != "" {
		contextsBuilder.WriteString("\n")
		contextsBuilder.WriteString(graphContext)
	}

	chatManage.RenderedContexts = contextsBuilder.String()

	// Replace placeholders in context template
	userContent := types.RenderPromptPlaceholders(chatManage.SummaryConfig.ContextTemplate, types.PlaceholderValues{
		"query":    safeQuery,
		"contexts": chatManage.RenderedContexts,
		"language": chatManage.Language,
	})
	if containsDataAnalysisResult(chatManage.MergeResult) {
		userContent += structuredAnswerOutputRules
	}

	// Append image description as text fallback only when the chat model cannot
	// process images directly. Vision-capable models see images via MultiContent.
	if chatManage.ImageDescription != "" && !chatManage.ChatModelSupportsVision {
		userContent += "\n\n[用户上传图片内容]\n" + chatManage.ImageDescription
	}
	if chatManage.QuotedContext != "" {
		userContent += "\n\n" + chatManage.QuotedContext
	}
	// Inject attachment content (documents, audio transcripts, etc.)
	if len(chatManage.Attachments) > 0 {
		userContent += chatManage.Attachments.BuildPrompt()
	}

	// Set formatted content back to chat management
	chatManage.UserContent = userContent
	pipelineInfo(ctx, "IntoChatMessage", "output", map[string]interface{}{
		"session_id":                 chatManage.SessionID,
		"user_content_len":           len(chatManage.UserContent),
		"faq_priority":               chatManage.FAQPriorityEnabled,
		"intent":                     chatManage.Intent,
		"image_description":          chatManage.ImageDescription,
		"chat_model_supports_vision": chatManage.ChatModelSupportsVision,
	})

	p.persistRenderedContent(ctx, chatManage)
	return next()
}

func containsDataAnalysisResult(results []*types.SearchResult) bool {
	for _, result := range results {
		if result != nil && result.MatchType == types.MatchTypeDataAnalysis {
			return true
		}
	}
	return false
}

// persistRenderedContent asynchronously writes the RAG-augmented UserContent back
// to the user message so that subsequent conversation turns can see the full
// retrieval context in history.
func (p *PluginIntoChatMessage) persistRenderedContent(ctx context.Context, chatManage *types.ChatManage) {
	if chatManage.UserMessageID == "" || chatManage.UserContent == "" {
		pipelineInfo(ctx, "IntoChatMessage", "persist_rendered_content_skip", map[string]interface{}{
			"session_id":       chatManage.SessionID,
			"user_message_id":  chatManage.UserMessageID,
			"has_user_content": chatManage.UserContent != "",
			"reason":           "empty_id_or_content",
		})
		return
	}
	if chatManage.UserContent == chatManage.Query {
		return
	}
	pipelineInfo(ctx, "IntoChatMessage", "persist_rendered_content", map[string]interface{}{
		"session_id":           chatManage.SessionID,
		"user_message_id":      chatManage.UserMessageID,
		"rendered_content_len": len(chatManage.UserContent),
	})
	bgCtx := context.WithoutCancel(ctx)
	go func() {
		if err := p.messageService.UpdateMessageRenderedContent(
			bgCtx, chatManage.SessionID, chatManage.UserMessageID, chatManage.UserContent,
		); err != nil {
			pipelineWarn(bgCtx, "IntoChatMessage", "persist_rendered_content_error", map[string]interface{}{
				"session_id":      chatManage.SessionID,
				"user_message_id": chatManage.UserMessageID,
				"error":           err.Error(),
			})
		}
	}()
}

// buildDocumentHeader generates a document metadata section listing each unique
// knowledge document (by KnowledgeID) with its title and description.
// Returns an empty string when no meaningful metadata is available.
func buildDocumentHeader(results []*types.SearchResult) string {
	type docMeta struct {
		title       string
		description string
	}

	seen := make(map[string]struct{})
	var docs []docMeta

	for _, r := range results {
		if r.KnowledgeID == "" {
			continue
		}
		if _, ok := seen[r.KnowledgeID]; ok {
			continue
		}
		seen[r.KnowledgeID] = struct{}{}

		title := r.KnowledgeTitle
		if title == "" {
			title = r.KnowledgeFilename
		}
		if title == "" {
			continue
		}

		docs = append(docs, docMeta{
			title:       title,
			description: r.KnowledgeDescription,
		})
	}

	if len(docs) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("<documents>\n")
	for _, d := range docs {
		b.WriteString("<document>\n")
		b.WriteString(fmt.Sprintf("<title>%s</title>\n", d.title))
		if d.description != "" {
			b.WriteString(fmt.Sprintf("<description>%s</description>\n", d.description))
		}
		b.WriteString("</document>\n")
	}
	b.WriteString("</documents>")
	return b.String()
}

// getEnrichedPassageForChat 合并Content和ImageInfo的文本内容，为聊天消息准备
func getEnrichedPassageForChat(ctx context.Context, result *types.SearchResult) string {
	// 如果没有图片信息，直接返回内容
	if result.Content == "" && result.ImageInfo == "" {
		return ""
	}

	// 如果只有内容，没有图片信息
	if result.ImageInfo == "" {
		return result.Content
	}

	// 处理图片信息并与内容合并
	return enrichContentWithImageInfo(ctx, result.Content, result.ImageInfo)
}

// enrichContentWithImageInfo delegates to the shared searchutil implementation.
func enrichContentWithImageInfo(_ context.Context, content string, imageInfoJSON string) string {
	return searchutil.EnrichContentWithImageInfo(content, imageInfoJSON)
}
