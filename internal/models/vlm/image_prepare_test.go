package vlm

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestPrepareImageForVLMSkipsWMF(t *testing.T) {
	data := []byte{0xD7, 0xCD, 0xC6, 0x9A, 0x00, 0x01}
	out, skip, err := PrepareImageForVLM(data, "minio://bucket/a.x-wmf")
	if err != nil || !skip || out != nil {
		t.Fatalf("wmf skip = out=%v skip=%v err=%v", out != nil, skip, err)
	}
}

func TestPrepareImageForVLMIgnoresMislabeledEMFHint(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	var raw bytes.Buffer
	if err := png.Encode(&raw, img); err != nil {
		t.Fatal(err)
	}
	out, skip, err := PrepareImageForVLM(raw.Bytes(), "uuid.emf")
	if err != nil || skip {
		t.Fatalf("mislabeled PNG must not skip VLM: skip=%v err=%v", skip, err)
	}
	if len(out) != len(raw.Bytes()) {
		t.Fatalf("expected original PNG bytes kept")
	}
}

func TestPrepareImageForVLMCompressesLargePNG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2200, 1600))
	for y := 0; y < 1600; y++ {
		for x := 0; x < 2200; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 40, A: 255})
		}
	}
	var raw bytes.Buffer
	if err := png.Encode(&raw, img); err != nil {
		t.Fatal(err)
	}
	out, skip, err := PrepareImageForVLM(raw.Bytes(), "big.png")
	if err != nil || skip {
		t.Fatalf("compress err=%v skip=%v", err, skip)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("expected jpeg output, decode err=%v len=%d", err, len(out))
	}
	if cfg.Width > vlmMaxImageEdge || cfg.Height > vlmMaxImageEdge {
		t.Fatalf("resized to %dx%d, want max edge %d", cfg.Width, cfg.Height, vlmMaxImageEdge)
	}
	if len(out) > vlmMaxImageBytes {
		t.Fatalf("prepared image still too large: %d", len(out))
	}
}

func TestPrepareImageForVLMCompressesOversizeBytes(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 900, 900))
	for y := 0; y < 900; y++ {
		for x := 0; x < 900; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * y), G: uint8(x + y), B: uint8(x ^ y), A: 255})
		}
	}
	var raw bytes.Buffer
	if err := jpeg.Encode(&raw, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	if len(raw.Bytes()) <= vlmMaxImageBytes {
		t.Fatalf("fixture too small to exercise byte cap: %d", len(raw.Bytes()))
	}
	out, skip, err := PrepareImageForVLM(raw.Bytes(), "noisy.png")
	if err != nil || skip {
		t.Fatalf("compress err=%v skip=%v", err, skip)
	}
	if len(out) >= len(raw.Bytes()) {
		t.Fatalf("expected smaller jpeg, in=%d out=%d", len(raw.Bytes()), len(out))
	}
}

func TestPrepareImageForVLMKeepsSmallPNG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	var raw bytes.Buffer
	if err := png.Encode(&raw, img); err != nil {
		t.Fatal(err)
	}
	out, skip, err := PrepareImageForVLM(raw.Bytes(), "small.png")
	if err != nil || skip {
		t.Fatalf("err=%v skip=%v", err, skip)
	}
	if len(out) != len(raw.Bytes()) {
		t.Fatalf("small image should stay unchanged")
	}
}
