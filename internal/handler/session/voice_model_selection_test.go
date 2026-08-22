package session

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/models/asr"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type voiceModelSelectionSessionService struct {
	interfaces.SessionService
}

func (voiceModelSelectionSessionService) GetSession(_ context.Context, id string) (*types.Session, error) {
	return &types.Session{ID: id, TenantID: 7}, nil
}

type voiceModelSelectionASR struct{}

func (voiceModelSelectionASR) Transcribe(_ context.Context, _ []byte, _ string) (*asr.TranscriptionResult, error) {
	return &asr.TranscriptionResult{Text: "selected model transcript"}, nil
}

func (voiceModelSelectionASR) GetModelName() string { return "selected" }
func (voiceModelSelectionASR) GetModelID() string   { return "asr-selected" }

type voiceModelSelectionModelService struct {
	interfaces.ModelService
	requestedIDs []string
	tenantIDs    []uint64
}

func (s *voiceModelSelectionModelService) GetASRModel(ctx context.Context, modelID string) (asr.ASR, error) {
	s.requestedIDs = append(s.requestedIDs, modelID)
	tenantID, _ := types.TenantIDFromContext(ctx)
	s.tenantIDs = append(s.tenantIDs, tenantID)
	return voiceModelSelectionASR{}, nil
}

func TestVoiceEndpointsUseRequestedASRModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	models := &voiceModelSelectionModelService{}
	handler := NewHandler(
		voiceModelSelectionSessionService{}, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, models, nil, nil,
	)
	router := gin.New()
	router.POST("/sessions/:session_id/voice/asr", handler.TranscribeVoiceBatch)
	router.GET("/sessions/:id/voice/ws", handler.VoiceWebSocket)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model_id", "asr-selected"); err != nil {
		t.Fatal(err)
	}
	audio, err := writer.CreateFormFile("audio", "recording.webm")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = audio.Write([]byte("audio")); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/sessions/voice-session/voice/asr", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("batch ASR status = %d, body = %s", response.Code, response.Body.String())
	}

	ticket, err := handler.voiceTickets.Issue("voice-user", 7, "voice-session", "asr", time.Minute, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(router)
	defer server.Close()
	connection, _, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(server.URL, "http")+"/sessions/voice-session/voice/ws?ticket="+ticket.Value,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err = connection.WriteJSON(map[string]string{"type": "start", "model_id": "asr-selected"}); err != nil {
		t.Fatal(err)
	}
	_, payload, err := connection.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var started struct {
		Type      string `json:"type"`
		ModelID   string `json:"model_id"`
		Streaming bool   `json:"streaming"`
	}
	if err = json.Unmarshal(payload, &started); err != nil {
		t.Fatal(err)
	}
	if started.Type != "started" || started.ModelID != "asr-selected" || started.Streaming {
		t.Fatalf("started response = %+v", started)
	}
	if len(models.requestedIDs) != 2 || models.requestedIDs[0] != "asr-selected" || models.requestedIDs[1] != "asr-selected" {
		t.Fatalf("requested model IDs = %#v", models.requestedIDs)
	}
	if len(models.tenantIDs) != 2 || models.tenantIDs[1] != 7 {
		t.Fatalf("model tenant IDs = %#v, want ticket tenant 7 for WebSocket", models.tenantIDs)
	}
}
