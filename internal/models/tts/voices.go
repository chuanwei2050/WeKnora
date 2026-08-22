package tts

import (
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

type VoiceOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type ListVoicesConfig struct {
	Provider      string
	ModelName     string
	BaseURL       string
	APIKey        string
	CustomHeaders map[string]string
	ValidateIP    func(net.IP) error
}

var openAIVoices = []VoiceOption{
	{Value: "alloy", Label: "alloy"},
	{Value: "ash", Label: "ash"},
	{Value: "ballad", Label: "ballad"},
	{Value: "coral", Label: "coral"},
	{Value: "echo", Label: "echo"},
	{Value: "fable", Label: "fable"},
	{Value: "nova", Label: "nova"},
	{Value: "onyx", Label: "onyx"},
	{Value: "sage", Label: "sage"},
	{Value: "shimmer", Label: "shimmer"},
	{Value: "verse", Label: "verse"},
}

var cosyVoiceSpeakers = []string{
	"alex", "anna", "bella", "benjamin", "charles", "claire", "david", "diana",
}

// ListVoices returns provider-aware preset voices. For self-hosted endpoints it
// also tries GET /v1/audio/voices when available.
func ListVoices(ctx context.Context, cfg ListVoicesConfig) []VoiceOption {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	modelName := strings.TrimSpace(cfg.ModelName)
	baseURL := strings.TrimSpace(cfg.BaseURL)

	if provider == "" && strings.Contains(strings.ToLower(baseURL), "siliconflow") {
		provider = "siliconflow"
	}

	switch provider {
	case "siliconflow":
		return siliconFlowVoices(modelName)
	case "openai":
		return append([]VoiceOption(nil), openAIVoices...)
	default:
		if remote := fetchRemoteVoices(ctx, cfg); len(remote) > 0 {
			return remote
		}
		return genericVoices()
	}
}

func siliconFlowVoices(modelName string) []VoiceOption {
	lower := strings.ToLower(modelName)
	speakers := cosyVoiceSpeakers
	if strings.Contains(lower, "moss") {
		speakers = []string{"alex"}
	}
	if modelName == "" {
		modelName = "FunAudioLLM/CosyVoice2-0.5B"
	}
	options := make([]VoiceOption, 0, len(speakers))
	for _, speaker := range speakers {
		value := fmt.Sprintf("%s:%s", modelName, speaker)
		options = append(options, VoiceOption{Value: value, Label: speaker})
	}
	return options
}

func genericVoices() []VoiceOption {
	return []VoiceOption{
		{Value: "default", Label: "default"},
		{Value: "alex", Label: "alex"},
	}
}

func fetchRemoteVoices(ctx context.Context, cfg ListVoicesConfig) []VoiceOption {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil
	}
	if err := transport.ValidateHTTPURL(cfg.BaseURL); err != nil {
		return nil
	}
	parsed, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	client := transport.NewHTTPClient(transport.Config{
		Timeout:      5 * time.Second,
		ValidateIP:   cfg.ValidateIP,
		AllowedHosts: []string{parsed.Hostname()},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/audio/voices", nil)
	if err != nil {
		return nil
	}
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	transport.ApplyHeaders(req, cfg.CustomHeaders)
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp != nil {
			resp.Body.Close()
		}
		return nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil
	}
	return parseRemoteVoices(body)
}

func parseRemoteVoices(body []byte) []VoiceOption {
	var direct struct {
		Voices []string `json:"voices"`
	}
	if err := json.Unmarshal(body, &direct); err == nil && len(direct.Voices) > 0 {
		return toVoiceOptions(direct.Voices)
	}
	var wrapped struct {
		Data struct {
			Voices []string `json:"voices"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil && len(wrapped.Data.Voices) > 0 {
		return toVoiceOptions(wrapped.Data.Voices)
	}
	return nil
}

func toVoiceOptions(values []string) []VoiceOption {
	options := make([]VoiceOption, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		options = append(options, VoiceOption{Value: value, Label: value})
	}
	return options
}
