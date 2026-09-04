package assets

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

func TestVisionCountChallengeEncodesExpectedCircles(t *testing.T) {
	baseData, err := CreateVisionCountChallenge(1)
	if err != nil {
		t.Fatalf("create base challenge: %v", err)
	}
	baseImage, err := png.Decode(bytes.NewReader(baseData))
	if err != nil {
		t.Fatalf("decode base challenge: %v", err)
	}
	redPixelsPerCircle := countRedPixels(baseImage)
	for count := 4; count <= 8; count++ {
		data, err := CreateVisionCountChallenge(count)
		if err != nil {
			t.Fatalf("create challenge with %d circles: %v", count, err)
		}
		decoded, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("decode challenge: %v", err)
		}
		if got := countRedPixels(decoded); got != redPixelsPerCircle*count {
			t.Fatalf("challenge has %d red pixels, want %d circles worth", got, count)
		}
	}
}

func countRedPixels(source image.Image) int {
	count := 0
	for y := source.Bounds().Min.Y; y < source.Bounds().Max.Y; y++ {
		for x := source.Bounds().Min.X; x < source.Bounds().Max.X; x++ {
			r, g, b, _ := source.At(x, y).RGBA()
			if r > 50000 && g < 15000 && b < 15000 {
				count++
			}
		}
	}
	return count
}
