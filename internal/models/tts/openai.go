package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/models/transport"
)

type Config struct {
	BaseURL       string
	APIKey        string
	ModelName     string
	ModelID       string
	Voice         string
	CustomHeaders map[string]string
	ValidateIP    func(net.IP) error
}

type OpenAITTS struct {
	config Config
	client *http.Client
}

func NewOpenAITTS(config Config) (*OpenAITTS, error) {
	if strings.TrimSpace(config.BaseURL) == "" {
		return nil, fmt.Errorf("tts base URL is required")
	}
	if err := transport.ValidateHTTPURL(config.BaseURL); err != nil {
		return nil, fmt.Errorf("invalid tts base URL: %w", err)
	}
	parsed, _ := url.Parse(config.BaseURL)
	return &OpenAITTS{config: config, client: transport.NewHTTPClient(transport.Config{Timeout: 5 * time.Minute, ValidateIP: config.ValidateIP, AllowedHosts: []string{parsed.Hostname()}})}, nil
}

func (s *OpenAITTS) Synthesize(ctx context.Context, text string, options SynthesizeOptions) (io.ReadCloser, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("tts text is empty")
	}
	if options.Voice == "" {
		options.Voice = s.config.Voice
	}
	if options.Format == "" {
		options.Format = "mp3"
	}
	payload := map[string]any{"model": s.config.ModelName, "input": text, "voice": options.Voice, "response_format": options.Format}
	if options.Speed > 0 {
		payload["speed"] = options.Speed
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(s.config.BaseURL, "/")
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/audio/speech", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.config.APIKey)
	}
	transport.ApplyHeaders(req, s.config.CustomHeaders)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tts request failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("tts request returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	return resp.Body, nil
}

func (s *OpenAITTS) GetModelName() string { return s.config.ModelName }
func (s *OpenAITTS) GetModelID() string   { return s.config.ModelID }
