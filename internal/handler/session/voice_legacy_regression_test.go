package session

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type voiceRegressionSessionService struct {
	interfaces.SessionService

	mu       sync.Mutex
	requests []*types.QARequest
}

func (s *voiceRegressionSessionService) GetSession(_ context.Context, id string) (*types.Session, error) {
	if id != "voice-regression" {
		return nil, errors.New("unexpected session")
	}
	return &types.Session{ID: id, TenantID: 7, Title: "voice regression"}, nil
}

func (s *voiceRegressionSessionService) KnowledgeQA(ctx context.Context, request *types.QARequest, bus *event.EventBus) error {
	s.mu.Lock()
	copyRequest := *request
	copyRequest.Attachments = append(types.MessageAttachments(nil), request.Attachments...)
	s.requests = append(s.requests, &copyRequest)
	s.mu.Unlock()

	return bus.Emit(ctx, event.Event{
		Type: event.EventAgentFinalAnswer,
		Data: event.AgentFinalAnswerData{Content: "reply: " + request.Query, Done: true},
	})
}

type voiceRegressionMessageService struct {
	interfaces.MessageService

	mu      sync.Mutex
	created []*types.Message
	updated []*types.Message
}

func (s *voiceRegressionMessageService) CreateMessage(_ context.Context, message *types.Message) (*types.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if message.ID == "" {
		message.ID = uuid.NewString()
	}
	copyMessage := *message
	copyMessage.Attachments = append(types.MessageAttachments(nil), message.Attachments...)
	s.created = append(s.created, &copyMessage)
	return message, nil
}

func (s *voiceRegressionMessageService) UpdateMessage(_ context.Context, message *types.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copyMessage := *message
	s.updated = append(s.updated, &copyMessage)
	return nil
}

func (s *voiceRegressionMessageService) IndexMessageToKB(context.Context, string, string, string, string) {
}

type voiceRegressionStreamManager struct {
	mu     sync.Mutex
	events []interfaces.StreamEvent
}

func (s *voiceRegressionStreamManager) AppendEvent(_ context.Context, _, _ string, event interfaces.StreamEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return nil
}

func (s *voiceRegressionStreamManager) GetEvents(_ context.Context, _, _ string, from int) ([]interfaces.StreamEvent, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if from >= len(s.events) {
		return nil, len(s.events), nil
	}
	events := append([]interfaces.StreamEvent(nil), s.events[from:]...)
	return events, len(s.events), nil
}

type voiceRegressionFileService struct {
	interfaces.FileService
}

func (voiceRegressionFileService) SaveBytes(_ context.Context, _ []byte, _ uint64, fileName string, _ bool) (string, error) {
	return "local://" + fileName, nil
}

func TestVoiceLegacyPathsKeepAudioAttachmentAndPlainTextChatWorking(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sessions := &voiceRegressionSessionService{}
	messages := &voiceRegressionMessageService{}
	stream := &voiceRegressionStreamManager{}
	handler := NewHandler(
		sessions,
		messages,
		stream,
		&config.Config{},
		nil,
		nil,
		nil,
		nil,
		voiceRegressionFileService{},
		nil,
		nil,
		nil,
	)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(types.TenantIDContextKey.String(), uint64(7))
		c.Set(types.UserIDContextKey.String(), "user-7")
		c.Set(types.RequestIDContextKey.String(), uuid.NewString())
		c.Next()
	})
	router.POST("/sessions/:session_id/knowledge-qa", handler.KnowledgeQA)

	audio := base64.StdEncoding.EncodeToString([]byte{0x01, 0x02, 0x03})
	voiceBody := `{"query":"audio question","disable_title":true,"attachment_uploads":[{"data":"` + audio + `","file_name":"question.mp3","file_size":3}]}`
	voiceResponse := httptest.NewRecorder()
	voiceRequest := httptest.NewRequest(http.MethodPost, "/sessions/voice-regression/knowledge-qa", strings.NewReader(voiceBody))
	voiceRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(voiceResponse, voiceRequest)
	if voiceResponse.Code != http.StatusOK {
		t.Fatalf("voice-compatible request status = %d, body = %s", voiceResponse.Code, voiceResponse.Body.String())
	}

	plainBody := `{"query":"plain text follow-up","disable_title":true}`
	plainResponse := httptest.NewRecorder()
	plainRequest := httptest.NewRequest(http.MethodPost, "/sessions/voice-regression/knowledge-qa", strings.NewReader(plainBody))
	plainRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(plainResponse, plainRequest)
	if plainResponse.Code != http.StatusOK {
		t.Fatalf("plain-text request status = %d, body = %s", plainResponse.Code, plainResponse.Body.String())
	}

	messages.mu.Lock()
	created := append([]*types.Message(nil), messages.created...)
	updated := append([]*types.Message(nil), messages.updated...)
	messages.mu.Unlock()
	if len(created) != 4 {
		t.Fatalf("created messages = %d, want two user/assistant pairs", len(created))
	}
	if created[0].Role != "user" || len(created[0].Attachments) != 1 {
		t.Fatalf("audio user message = %+v, want one persisted attachment", created[0])
	}
	attachment := created[0].Attachments[0]
	if attachment.FileName != "question.mp3" || attachment.FileType != ".mp3" || !strings.Contains(attachment.Content, "audio_file") {
		t.Fatalf("audio attachment = %+v", attachment)
	}
	if created[2].Role != "user" || created[2].Content != "plain text follow-up" || len(created[2].Attachments) != 0 {
		t.Fatalf("plain-text user message = %+v", created[2])
	}
	if len(updated) < 2 {
		t.Fatalf("updated assistant messages = %d, want both answers completed", len(updated))
	}
	for _, message := range updated {
		if !message.IsCompleted || !strings.HasPrefix(message.Content, "reply: ") {
			t.Fatalf("incomplete assistant message = %+v", message)
		}
	}

	sessions.mu.Lock()
	requests := append([]*types.QARequest(nil), sessions.requests...)
	sessions.mu.Unlock()
	if len(requests) != 2 || len(requests[0].Attachments) != 1 || len(requests[1].Attachments) != 0 {
		t.Fatalf("QA request attachment continuity = %+v", requests)
	}

	stream.mu.Lock()
	events := append([]interfaces.StreamEvent(nil), stream.events...)
	stream.mu.Unlock()
	completed := 0
	for _, streamEvent := range events {
		if streamEvent.Type == types.ResponseTypeComplete && streamEvent.Done {
			completed++
		}
	}
	if completed != 2 {
		t.Fatalf("completed SSE streams = %d, want 2", completed)
	}
}
