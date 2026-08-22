package tts

import (
	"context"
	"testing"
)

func TestSiliconFlowCosyVoiceVoices(t *testing.T) {
	voices := ListVoices(context.Background(), ListVoicesConfig{
		Provider:  "siliconflow",
		ModelName: "FunAudioLLM/CosyVoice2-0.5B",
	})
	if len(voices) != len(cosyVoiceSpeakers) {
		t.Fatalf("voices len = %d, want %d", len(voices), len(cosyVoiceSpeakers))
	}
	if voices[0].Value != "FunAudioLLM/CosyVoice2-0.5B:alex" {
		t.Fatalf("first voice = %q", voices[0].Value)
	}
}

func TestOpenAIVoices(t *testing.T) {
	voices := ListVoices(context.Background(), ListVoicesConfig{Provider: "openai"})
	if len(voices) == 0 {
		t.Fatal("expected openai voices")
	}
	if voices[0].Value != "alloy" {
		t.Fatalf("first voice = %q, want alloy", voices[0].Value)
	}
}

func TestGenericVoicesFallback(t *testing.T) {
	voices := ListVoices(context.Background(), ListVoicesConfig{
		Provider:  "generic",
		BaseURL:   "http://127.0.0.1:65535/v1",
		ModelName: "cosyvoice2-0.5b",
	})
	if len(voices) != 2 || voices[0].Value != "default" {
		t.Fatalf("generic voices = %+v", voices)
	}
}

func TestParseRemoteVoices(t *testing.T) {
	options := parseRemoteVoices([]byte(`{"voices":["default","alex"]}`))
	if len(options) != 2 || options[1].Value != "alex" {
		t.Fatalf("parsed voices = %+v", options)
	}
}
