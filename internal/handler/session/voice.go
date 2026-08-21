package session

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/models/asr"
	"github.com/Tencent/WeKnora/internal/models/tts"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type voiceTicketRequest struct {
	Purpose string `json:"purpose"`
}

// TranscribeVoiceBatch is the bounded fallback used when a streaming ASR
// model is unavailable. It only returns text and never creates an attachment
// or an object-storage record.
func (h *Handler) TranscribeVoiceBatch(c *gin.Context) {
	ctx := c.Request.Context()
	sessionID := strings.TrimSpace(c.Param("session_id"))
	if _, err := h.sessionService.GetSession(ctx, sessionID); err != nil {
		c.Error(errors.NewNotFoundError("session not found"))
		return
	}
	modelID := strings.TrimSpace(c.PostForm("model_id"))
	if modelID == "" {
		c.Error(errors.NewBadRequestError("model_id is required"))
		return
	}
	header, err := c.FormFile("audio")
	if err != nil || header == nil {
		c.Error(errors.NewBadRequestError("audio file is required"))
		return
	}
	if header.Size <= 0 || header.Size > 25<<20 {
		c.Error(errors.NewBadRequestError("audio file must be between 1 byte and 25 MiB"))
		return
	}
	file, err := header.Open()
	if err != nil {
		c.Error(errors.NewBadRequestError("audio file cannot be opened"))
		return
	}
	defer file.Close()
	audio, err := io.ReadAll(io.LimitReader(file, (25<<20)+1))
	if err != nil || len(audio) > 25<<20 {
		c.Error(errors.NewBadRequestError("audio file is too large"))
		return
	}
	model, err := h.modelService.GetASRModel(ctx, modelID)
	if err != nil {
		c.Error(errors.NewBadRequestError("asr model is unavailable"))
		return
	}
	result, err := model.Transcribe(ctx, audio, header.Filename)
	if err != nil {
		c.Error(errors.NewInternalServerError("audio transcription failed"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"text": result.Text, "segments": result.Segments})
}

type VoiceSynthesisRequest struct {
	MessageID string  `json:"message_id" binding:"required"`
	ModelID   string  `json:"model_id" binding:"required"`
	Language  string  `json:"language,omitempty"`
	Voice     string  `json:"voice,omitempty"`
	Speed     float64 `json:"speed,omitempty"`
	Format    string  `json:"format,omitempty"`
}

// IssueVoiceWSTicket issues a short-lived, single-use ticket. The ticket is
// scoped to the authenticated user, session and declared purpose.
func (h *Handler) IssueVoiceWSTicket(c *gin.Context) {
	ctx := c.Request.Context()
	sessionID := strings.TrimSpace(c.Param("session_id"))
	if sessionID == "" {
		c.Error(errors.NewBadRequestError("session_id is required"))
		return
	}
	if _, err := h.sessionService.GetSession(ctx, sessionID); err != nil {
		c.Error(errors.NewNotFoundError("session not found"))
		return
	}
	userID := c.GetString(types.UserIDContextKey.String())
	if userID == "" {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	var request voiceTicketRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			c.Error(errors.NewBadRequestError("invalid voice ticket request"))
			return
		}
	}
	purpose := strings.TrimSpace(request.Purpose)
	if purpose == "" {
		purpose = "asr"
	}
	tenantID := types.MustTenantIDFromContext(ctx)
	ticket, err := h.voiceTickets.Issue(userID, tenantID, sessionID, purpose, h.config.Voice.WSTicketTTL, time.Now())
	if err != nil {
		c.Error(errors.NewInternalServerError("failed to issue voice ticket"))
		return
	}
	c.JSON(http.StatusCreated, gin.H{"ticket": ticket.Value, "expires_at": ticket.ExpiresAt, "purpose": ticket.Purpose})
}

type voiceSocketMessage struct {
	Type     string `json:"type"`
	ModelID  string `json:"model_id,omitempty"`
	FileName string `json:"file_name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
}

var voiceSocketUpgrader = websocket.Upgrader{
	ReadBufferSize:  32 * 1024,
	WriteBufferSize: 32 * 1024,
	CheckOrigin: func(request *http.Request) bool {
		origin := strings.TrimSpace(request.Header.Get("Origin"))
		if origin == "" {
			return true
		}
		parsed, err := url.Parse(origin)
		return err == nil && strings.EqualFold(parsed.Host, request.Host)
	},
}

// VoiceWebSocket consumes a one-time ticket and supports the explicit
// start/audio/stop/cancel protocol. Batch-only ASR providers still work via
// the same endpoint; they emit a final transcript on stop and never persist
// the buffered audio.
func (h *Handler) VoiceWebSocket(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Param("id"))
	ticket := strings.TrimSpace(c.Query("ticket"))
	if sessionID == "" || ticket == "" {
		c.Error(errors.NewUnauthorizedError("voice WebSocket ticket is required"))
		return
	}
	identity, err := h.voiceTickets.ConsumeForSession(ticket, sessionID, "asr", time.Now())
	if err != nil {
		c.Error(errors.NewUnauthorizedError("voice WebSocket ticket is invalid"))
		return
	}
	c.Set(types.UserIDContextKey.String(), identity.UserID)
	c.Set(types.TenantIDContextKey.String(), identity.TenantID)
	requestContext := context.WithValue(c.Request.Context(), types.UserIDContextKey, identity.UserID)
	requestContext = context.WithValue(requestContext, types.TenantIDContextKey, identity.TenantID)
	c.Request = c.Request.WithContext(requestContext)
	conn, err := voiceSocketUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	var streamingSession asr.StreamingSession
	defer func() {
		if streamingSession != nil {
			_ = streamingSession.Close()
		}
	}()
	conn.SetReadLimit(25 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	var model asr.ASR
	var modelID, fileName string
	var startedAt time.Time
	var audio []byte
	started := false
	for {
		messageType, payload, readErr := conn.ReadMessage()
		if readErr != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		if messageType == websocket.BinaryMessage {
			if !started || len(payload) > 1<<20 || len(audio)+len(payload) > 25<<20 {
				_ = conn.WriteJSON(gin.H{"type": "error", "message": "recording is not started or exceeds 25 MiB"})
				continue
			}
			if time.Since(startedAt) > 5*time.Minute {
				_ = conn.WriteJSON(gin.H{"type": "error", "message": "recording exceeds 5 minutes"})
				return
			}
			audio = append(audio, payload...)
			if streamingSession != nil {
				if writeErr := streamingSession.Write(payload); writeErr != nil {
					_ = conn.WriteJSON(gin.H{"type": "error", "message": "failed to stream audio"})
					return
				}
				if partial, ok := streamingSession.(asr.PartialStreamingSession); ok {
					if result, partialErr := partial.Partial(c.Request.Context()); partialErr == nil && result != nil && strings.TrimSpace(result.Text) != "" {
						_ = conn.WriteJSON(gin.H{"type": "partial", "text": result.Text})
					}
				}
			}
			continue
		}
		var message voiceSocketMessage
		if err := json.Unmarshal(payload, &message); err != nil {
			_ = conn.WriteJSON(gin.H{"type": "error", "message": "invalid voice message"})
			continue
		}
		switch strings.ToLower(strings.TrimSpace(message.Type)) {
		case "start":
			if started {
				_ = conn.WriteJSON(gin.H{"type": "error", "message": "recording is already started"})
				continue
			}
			if strings.TrimSpace(message.ModelID) == "" {
				_ = conn.WriteJSON(gin.H{"type": "error", "message": "model_id is required"})
				continue
			}
			if message.MimeType != "" && !supportedVoiceMIME(message.MimeType) {
				_ = conn.WriteJSON(gin.H{"type": "error", "message": "unsupported audio mime type"})
				continue
			}
			resolved, resolveErr := h.modelService.GetASRModel(c.Request.Context(), message.ModelID)
			if resolveErr != nil {
				_ = conn.WriteJSON(gin.H{"type": "error", "message": "asr model is unavailable"})
				continue
			}
			model, modelID, fileName, audio, started, startedAt = resolved, message.ModelID, message.FileName, audio[:0], true, time.Now()
			if factory, ok := model.(asr.StreamingSessionFactory); ok && factory.SupportsStreaming() {
				streamingSession, resolveErr = factory.NewStreamingSession(c.Request.Context())
				if resolveErr != nil {
					streamingSession = nil
				}
			}
			_ = conn.WriteJSON(gin.H{"type": "started", "model_id": modelID, "streaming": streamingSession != nil})
		case "stop":
			if !started || model == nil || len(audio) == 0 {
				_ = conn.WriteJSON(gin.H{"type": "error", "message": "recording has no audio"})
				continue
			}
			var result *asr.TranscriptionResult
			var transcribeErr error
			if streamingSession != nil {
				result, transcribeErr = streamingSession.Finalize(c.Request.Context())
				_ = streamingSession.Close()
			} else {
				result, transcribeErr = model.Transcribe(c.Request.Context(), audio, fileName)
			}
			if transcribeErr != nil {
				_ = conn.WriteJSON(gin.H{"type": "error", "message": "audio transcription failed"})
			} else {
				_ = conn.WriteJSON(gin.H{"type": "final", "text": result.Text, "segments": result.Segments})
			}
			return
		case "cancel":
			_ = conn.WriteJSON(gin.H{"type": "cancelled"})
			return
		default:
			_ = conn.WriteJSON(gin.H{"type": "error", "message": "unsupported voice message type"})
		}
	}
}

func supportedVoiceMIME(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "audio/webm", "audio/webm;codecs=opus", "audio/ogg", "audio/ogg;codecs=opus", "audio/mp4", "audio/mpeg", "audio/wav", "audio/x-wav":
		return true
	default:
		return false
	}
}

// SynthesizeVoice streams audio for an authorized final assistant message. No
// audio is persisted; the response body is the only copy returned to the user.
func (h *Handler) SynthesizeVoice(c *gin.Context) {
	ctx := c.Request.Context()
	sessionID := strings.TrimSpace(c.Param("session_id"))
	var request VoiceSynthesisRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(errors.NewBadRequestError("invalid voice synthesis request"))
		return
	}
	if _, err := h.sessionService.GetSession(ctx, sessionID); err != nil {
		c.Error(errors.NewNotFoundError("session not found"))
		return
	}
	message, err := h.messageService.GetMessage(ctx, sessionID, request.MessageID)
	if err != nil || message == nil || message.Role != "assistant" || !message.IsCompleted {
		c.Error(errors.NewNotFoundError("completed assistant message not found"))
		return
	}
	SynthesizeVoiceMessage(c, h.modelService, message, request)
}

// SynthesizeVoiceMessage streams synthesized audio for a message that the
// caller has already authorized.
func SynthesizeVoiceMessage(c *gin.Context, modelService interfaces.ModelService, message *types.Message, request VoiceSynthesisRequest) {
	ctx := c.Request.Context()
	model, err := modelService.GetTTSModel(ctx, request.ModelID)
	if err != nil {
		c.Error(errors.NewBadRequestError("tts model is unavailable"))
		return
	}
	format := strings.ToLower(strings.TrimSpace(request.Format))
	if format == "" {
		format = "mp3"
	}
	options := tts.SynthesizeOptions{Language: request.Language, Voice: request.Voice, Speed: request.Speed, Format: format}
	if err := options.Validate(); err != nil {
		c.Error(errors.NewBadRequestError("invalid voice synthesis options"))
		return
	}
	if format != "mp3" {
		stream, synthesizeErr := model.Synthesize(ctx, message.Content, options)
		if synthesizeErr != nil {
			c.Error(errors.NewInternalServerError("voice synthesis failed"))
			return
		}
		defer stream.Close()
		c.DataFromReader(http.StatusOK, -1, ttsContentType(format), stream, nil)
		return
	}

	chunks := splitTTSContent(message.Content, 120)
	if len(chunks) == 0 {
		c.Error(errors.NewBadRequestError("message content is empty"))
		return
	}

	var stream io.ReadCloser
	for index, chunk := range chunks {
		stream, err = model.Synthesize(ctx, chunk, options)
		if err != nil {
			if index == 0 {
				c.Error(errors.NewInternalServerError("voice synthesis failed"))
			}
			return
		}
		if index == 0 {
			c.Header("Content-Type", ttsContentType(format))
			c.Header("Cache-Control", "no-store")
			c.Header("X-Accel-Buffering", "no")
			c.Status(http.StatusOK)
		}
		_, copyErr := io.Copy(c.Writer, stream)
		closeErr := stream.Close()
		c.Writer.Flush()
		if copyErr != nil || closeErr != nil || ctx.Err() != nil {
			return
		}
	}
}

func splitTTSContent(content string, maxRunes int) []string {
	content = strings.TrimSpace(content)
	if content == "" || maxRunes <= 0 {
		return nil
	}

	chunks := make([]string, 0, len(content)/maxRunes+1)
	current := make([]rune, 0, maxRunes)
	flush := func() {
		chunk := strings.TrimSpace(string(current))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		current = current[:0]
	}
	for _, value := range []rune(content) {
		current = append(current, value)
		if isTTSBoundary(value) || len(current) >= maxRunes {
			flush()
		}
	}
	flush()
	return chunks
}

func isTTSBoundary(value rune) bool {
	switch value {
	case '。', '！', '？', '；', '.', '!', '?', ';', '\n':
		return true
	default:
		return false
	}
}

func ttsContentType(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "wav":
		return "audio/wav"
	case "opus":
		return "audio/opus"
	case "pcm":
		return "audio/pcm"
	default:
		return "audio/mpeg"
	}
}
