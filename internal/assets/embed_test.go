package assets

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"testing"
)

func TestVisionTestPNGDimensions(t *testing.T) {
	data, err := base64.StdEncoding.DecodeString(VisionTestPNGBase64)
	if err != nil {
		t.Fatalf("decode vision test image: %v", err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode vision test image config: %v", err)
	}
	if config.Width != 64 || config.Height != 64 {
		t.Fatalf("unexpected dimensions: %dx%d", config.Width, config.Height)
	}
}
