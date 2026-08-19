package session

import (
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
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	defer file.Close()
	audio, err := io.ReadAll(io.LimitReader(file, (25<<20)+1))
	if err != nil || len(audio) > 25<<20 {
		c.Error(errors.NewBadRequestError("audio file is too large"))
		return
	}
	model, err := h.modelService.GetASRModel(ctx, "")
	if err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	result, err := model.Transcribe(ctx, audio, header.Filename)
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"text": result.Text, "segments": result.Segments})
}

type voiceSynthesisRequest struct {
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
			c.Error(errors.NewBadRequestError(err.Error()))
			return
		}
	}
	purpose := strings.TrimSpace(request.Purpose)
	if purpose == "" {
		purpose = "asr"
	}
	ticket, err := h.voiceTickets.Issue(userID, sessionID, purpose, h.config.Voice.WSTicketTTL, time.Now())
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
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
	userID := c.GetString(types.UserIDContextKey.String())
	sessionID := strings.TrimSpace(c.Param("id"))
	ticket := strings.TrimSpace(c.Query("ticket"))
	if userID == "" || sessionID == "" || ticket == "" {
		c.Error(errors.NewUnauthorizedError("voice WebSocket ticket is required"))
		return
	}
	if _, err := h.voiceTickets.Consume(ticket, userID, sessionID, "asr", time.Now()); err != nil {
		c.Error(errors.NewUnauthorizedError("voice WebSocket ticket is invalid"))
		return
	}
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
					_ = conn.WriteJSON(gin.H{"type": "error", "message": writeErr.Error()})
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
			resolved, resolveErr := h.modelService.GetASRModel(c.Request.Context(), "")
			if resolveErr != nil {
				_ = conn.WriteJSON(gin.H{"type": "error", "message": resolveErr.Error()})
				continue
			}
			model, modelID, fileName, audio, started, startedAt = resolved, message.ModelID, message.FileName, audio[:0], true, time.Now()
			if factory, ok := model.(asr.StreamingSessionFactory); ok && factory.SupportsStreaming() {
				streamingSession, resolveErr = factory.NewStreamingSession(c.Request.Context())
				if resolveErr != nil {
					streamingSession = nil
				}
			}
			_ = conn.WriteJSON(gin.H{"type": "started", "model_id": modelID})
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
				_ = conn.WriteJSON(gin.H{"type": "error", "message": transcribeErr.Error()})
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
	var request voiceSynthesisRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
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
	model, err := h.modelService.GetTTSModel(ctx, "")
	if err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	options := tts.SynthesizeOptions{Language: request.Language, Voice: request.Voice, Speed: request.Speed, Format: request.Format}
	if err := options.Validate(); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	stream, err := model.Synthesize(ctx, message.Content, options)
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	defer stream.Close()
	c.DataFromReader(http.StatusOK, -1, ttsContentType(request.Format), stream, nil)
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
