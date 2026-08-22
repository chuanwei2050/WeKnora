package session

import "testing"

func TestTTSContentType(t *testing.T) {
	tests := map[string]string{
		"":     "audio/mpeg",
		"mp3":  "audio/mpeg",
		"wav":  "audio/wav",
		"opus": "audio/opus",
		"pcm":  "audio/pcm",
	}
	for format, expected := range tests {
		if got := ttsContentType(format); got != expected {
			t.Errorf("ttsContentType(%q) = %q, want %q", format, got, expected)
		}
	}
}
