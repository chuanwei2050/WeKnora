package chatpipeline

import (
	"context"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/common"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
)

var regThinkTags = regexp.MustCompile(`(?s)<think>.*?</think>`)
var regQuotedMessage = regexp.MustCompile(`(?s)<quoted_message>.*?</quoted_message>`)

// pipelineInfo logs pipeline info level entries.
func pipelineInfo(ctx context.Context, stage, action string, fields map[string]interface{}) {
	common.PipelineInfo(ctx, stage, action, fields)
}

// pipelineWarn logs pipeline warning level entries.
func pipelineWarn(ctx context.Context, stage, action string, fields map[string]interface{}) {
	common.PipelineWarn(ctx, stage, action, fields)
}

// pipelineError logs pipeline error level entries.
func pipelineError(ctx context.Context, stage, action string, fields map[string]interface{}) {
	common.PipelineError(ctx, stage, action, fields)
}

func emitPipelineStageStart(ctx context.Context, chatManage *types.ChatManage, name, hint string) (string, time.Time) {
	started := time.Now()
	if chatManage.EventBus == nil {
		return "", started
	}
	id := uuid.NewString()
	chatManage.EventBus.Emit(ctx, types.Event{Type: types.EventType(event.EventAgentToolCall), SessionID: chatManage.SessionID, Data: event.AgentToolCallData{ToolCallID: id, ToolName: name, Hint: hint, Arguments: map[string]any{"pipeline_stage": true}}})
	return id, started
}

func emitPipelineStageResult(ctx context.Context, chatManage *types.ChatManage, id, name, output string, started time.Time, success bool, stageData ...map[string]interface{}) {
	if chatManage.EventBus == nil || id == "" {
		return
	}
	data := map[string]interface{}{"pipeline_stage": true}
	if len(stageData) > 0 {
		for key, value := range stageData[0] {
			data[key] = value
		}
	}
	chatManage.EventBus.Emit(ctx, types.Event{Type: types.EventType(event.EventAgentToolResult), SessionID: chatManage.SessionID, Data: event.AgentToolResultData{ToolCallID: id, ToolName: name, Output: output, Success: success, Duration: time.Since(started).Milliseconds(), Data: data}})
}

// prepareChatModel shared logic to prepare chat model and options
// it gets the chat model and sets up the chat options based on the chat manage.
func prepareChatModel(ctx context.Context, modelService interfaces.ModelService,
	chatManage *types.ChatManage,
) (chat.Chat, *chat.ChatOptions, error) {
	chatModel, err := modelService.GetChatModel(ctx, chatManage.ChatModelID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get chat model: %v", err)
		return nil, nil, err
	}

	opt := &chat.ChatOptions{
		Temperature:         chatManage.SummaryConfig.Temperature,
		TopP:                chatManage.SummaryConfig.TopP,
		Seed:                chatManage.SummaryConfig.Seed,
		MaxTokens:           chatManage.SummaryConfig.MaxTokens,
		MaxCompletionTokens: chatManage.SummaryConfig.MaxCompletionTokens,
		FrequencyPenalty:    chatManage.SummaryConfig.FrequencyPenalty,
		PresencePenalty:     chatManage.SummaryConfig.PresencePenalty,
		Thinking:            chatManage.SummaryConfig.Thinking,
	}

	return chatModel, opt, nil
}

// prepareMessagesWithHistory prepare complete messages including history.
// When SystemPromptOverride is set (e.g. by intent-specific prompt logic),
// it takes precedence over the default SummaryConfig.Prompt.
func prepareMessagesWithHistory(chatManage *types.ChatManage) []chat.Message {
	base := chatManage.SummaryConfig.Prompt
	if chatManage.SystemPromptOverride != "" {
		base = chatManage.SystemPromptOverride
	}
	systemPrompt := types.RenderPromptPlaceholders(base, types.PlaceholderValues{
		"query":    chatManage.Query,
		"language": chatManage.Language,
		"contexts": chatManage.RenderedContexts,
	})

	chatMessages := []chat.Message{
		{Role: "system", Content: systemPrompt},
	}

	// Add conversation history (already limited by maxRounds in load_history/rewrite plugins)
	for _, history := range chatManage.History {
		query := history.Query
		if history.OriginalQuery != "" {
			query = history.OriginalQuery
		}
		chatMessages = append(chatMessages, chat.Message{Role: "user", Content: query})
		chatMessages = append(chatMessages, chat.Message{Role: "assistant", Content: history.Answer})
	}

	// Add current user message. Only include images when the chat model supports
	// vision; non-vision models rely on the text description in UserContent.
	userMsg := chat.Message{Role: "user", Content: chatManage.UserContent}
	if chatManage.ChatModelSupportsVision && len(chatManage.Images) > 0 {
		userMsg.Images = chatManage.Images
	}
	chatMessages = append(chatMessages, userMsg)

	return chatMessages
}

// loadAndProcessHistory fetches recent messages, groups them into Q&A pairs,
// strips <think> tags from assistant answers, sorts by recency, and limits to maxRounds.
// fetchCount controls how many raw messages to fetch (typically maxRounds*2+10).
func loadAndProcessHistory(
	ctx context.Context,
	messageService interfaces.MessageService,
	sessionID string,
	maxRounds int,
	fetchCount int,
) ([]*types.History, error) {
	history, err := messageService.GetRecentMessagesBySession(ctx, sessionID, fetchCount)
	if err != nil {
		return nil, err
	}

	historyMap := make(map[string]*types.History)
	for _, message := range history {
		h, ok := historyMap[message.RequestID]
		if !ok {
			h = &types.History{}
		}
		if message.Role == "user" {
			h.OriginalQuery = historicalUserContent(message)
			if message.RenderedContent != "" {
				h.Query = message.RenderedContent
			} else {
				h.Query = message.Content
			}
			h.CreateAt = message.CreatedAt
			if desc := extractImageCaptions(message.Images); desc != "" && message.RenderedContent == "" {
				h.Query += "\n\n[用户上传图片内容]\n" + desc
			}
		} else {
			h.Answer = regThinkTags.ReplaceAllString(message.Content, "")
			h.KnowledgeReferences = message.KnowledgeReferences
		}
		historyMap[message.RequestID] = h
	}

	historyList := make([]*types.History, 0, len(historyMap))
	for _, h := range historyMap {
		if h.Answer != "" && h.Query != "" {
			historyList = append(historyList, h)
		}
	}

	sort.Slice(historyList, func(i, j int) bool {
		return historyList[i].CreateAt.After(historyList[j].CreateAt)
	})

	if len(historyList) > maxRounds {
		historyList = historyList[:maxRounds]
	}

	slices.Reverse(historyList)
	return historyList, nil
}

// historicalUserContent rebuilds the non-RAG parts of a persisted user turn.
// RenderedContent may contain retrieved evidence that must not leak into later
// turns, while images, quoted messages and attachments remain valid context.
func historicalUserContent(message *types.Message) string {
	content := message.Content
	if desc := extractImageCaptions(message.Images); desc != "" {
		content += "\n\n[用户上传图片内容]\n" + desc
	}
	if quoted := regQuotedMessage.FindString(message.RenderedContent); quoted != "" {
		content += "\n\n" + quoted
	}
	content += message.Attachments.BuildPrompt()
	return content
}

// extractImageCaptions concatenates non-empty Caption fields from stored
// message images. Used when loading history so that previous turns' image
// descriptions are visible to the model.
func extractImageCaptions(images types.MessageImages) string {
	var parts []string
	for _, img := range images {
		if img.Caption != "" {
			parts = append(parts, img.Caption)
		}
	}
	return strings.Join(parts, "\n")
}

// ---------------------------------------------------------------------------
// Concurrency utilities
// ---------------------------------------------------------------------------

// ParallelTask represents a named unit of concurrent work.
type ParallelTask struct {
	Name string
	Run  func() *PluginError
}

// RunParallel executes tasks concurrently.
// Returns a map of task name → error for tasks that returned non-nil errors.
func RunParallel(tasks ...ParallelTask) map[string]*PluginError {
	errs := make(map[string]*PluginError)
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(len(tasks))
	for _, task := range tasks {
		go func(t ParallelTask) {
			defer wg.Done()
			if err := t.Run(); err != nil {
				mu.Lock()
				errs[t.Name] = err
				mu.Unlock()
			}
		}(task)
	}
	wg.Wait()
	return errs
}

// ParallelMap applies fn to each element of items concurrently (up to
// maxWorkers goroutines) and returns results in the same order as items.
// If maxWorkers <= 0, concurrency is unbounded (one goroutine per item).
func ParallelMap[T, R any](items []T, maxWorkers int, fn func(int, T) R) []R {
	n := len(items)
	if n == 0 {
		return nil
	}
	results := make([]R, n)

	if maxWorkers <= 0 || maxWorkers > n {
		maxWorkers = n
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxWorkers)

	for i, item := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, it T) {
			defer func() { <-sem; wg.Done() }()
			results[idx] = fn(idx, it)
		}(i, item)
	}
	wg.Wait()
	return results
}
