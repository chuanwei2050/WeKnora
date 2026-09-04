package assets

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
)

// CreateVisionCountChallenge returns a high-contrast image containing count red circles.
func CreateVisionCountChallenge(count int) ([]byte, error) {
	if count < 1 || count > 9 {
		return nil, fmt.Errorf("invalid vision challenge count: %d", count)
	}
	canvas := image.NewRGBA(image.Rect(0, 0, 512, 512))
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	red := color.RGBA{R: 220, G: 30, B: 30, A: 255}
	for y := 0; y < 512; y++ {
		for x := 0; x < 512; x++ {
			canvas.SetRGBA(x, y, white)
		}
	}
	for index := 0; index < count; index++ {
		centerX := 96 + (index%3)*160
		centerY := 96 + (index/3)*160
		for y := centerY - 36; y <= centerY+36; y++ {
			for x := centerX - 36; x <= centerX+36; x++ {
				dx, dy := x-centerX, y-centerY
				if dx*dx+dy*dy <= 36*36 {
					canvas.SetRGBA(x, y, red)
				}
			}
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}
