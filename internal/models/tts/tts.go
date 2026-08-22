package tts

import (
	"context"
	"fmt"
	"io"
	"strings"
	"unicode"
)

type SynthesizeOptions struct {
	Language string  `json:"language,omitempty"`
	Voice    string  `json:"voice,omitempty"`
	Speed    float64 `json:"speed,omitempty"`
	Format   string  `json:"format,omitempty"`
}

func (o SynthesizeOptions) Validate() error {
	if o.Speed < 0 || o.Speed > 4 {
		return fmt.Errorf("tts speed must be between 0 and 4")
	}
	if o.Format != "" && o.Format != "mp3" && o.Format != "wav" && o.Format != "opus" && o.Format != "pcm" {
		return fmt.Errorf("unsupported tts output format %q", o.Format)
	}
	for name, value := range map[string]string{"language": o.Language, "voice": o.Voice} {
		if strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return fmt.Errorf("tts %s contains control characters", name)
		}
	}
	return nil
}

type TTS interface {
	Synthesize(context.Context, string, SynthesizeOptions) (io.ReadCloser, error)
	GetModelName() string
	GetModelID() string
}
