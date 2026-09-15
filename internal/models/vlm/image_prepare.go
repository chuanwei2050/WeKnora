package vlm

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
)

const (
	// Max edge length sent to remote VLMs. Larger images are downscaled.
	vlmMaxImageEdge = 1536
	// Images larger than this (after optional decode) are re-encoded as JPEG.
	vlmMaxImageBytes = 900 * 1024
	vlmJPEGQuality   = 75
)

// PrepareImageForVLM normalizes image bytes for OpenAI-compatible vision APIs.
// WMF/EMF are not vision-model inputs — callers should use ExtractMetafileText
// instead; PrepareImageForVLM returns skip=true for those formats.
func PrepareImageForVLM(data []byte, sourceHint string) (out []byte, skip bool, err error) {
	if len(data) == 0 {
		return nil, true, nil
	}
	if isUnsupportedVLMImage(data, sourceHint) {
		return nil, true, nil
	}

	img, _, decodeErr := image.Decode(bytes.NewReader(data))
	if decodeErr != nil {
		// Keep original bytes for formats the stdlib cannot decode but the
		// remote endpoint might still accept (e.g. some webp via proxy).
		if len(data) <= vlmMaxImageBytes {
			return data, false, nil
		}
		return nil, false, fmt.Errorf("image too large (%d bytes) and not decodable for compression: %w", len(data), decodeErr)
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	needsResize := w > vlmMaxImageEdge || h > vlmMaxImageEdge
	if !needsResize && len(data) <= vlmMaxImageBytes {
		return data, false, nil
	}

	if needsResize {
		img = resizeImageMaxEdge(img, vlmMaxImageEdge)
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: vlmJPEGQuality}); err != nil {
		return nil, false, fmt.Errorf("jpeg encode for VLM: %w", err)
	}
	encoded := buf.Bytes()
	// Prefer the smaller payload when we only compressed for size (no resize).
	if !needsResize && len(encoded) >= len(data) {
		return data, false, nil
	}
	return encoded, false, nil
}

func isUnsupportedVLMImage(data []byte, sourceHint string) bool {
	// Prefer magic over path/MIME hints: stored objects may keep an .emf/.wmf
	// extension while the payload is a normal raster.
	if isEMF(data) || isWMF(data) {
		return true
	}
	_ = sourceHint
	return false
}

func resizeImageMaxEdge(img image.Image, maxEdge int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxEdge && h <= maxEdge {
		return img
	}
	scale := float64(maxEdge) / float64(w)
	if h > w {
		scale = float64(maxEdge) / float64(h)
	}
	nw := int(float64(w) * scale)
	nh := int(float64(h) * scale)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy := b.Min.Y + y*h/nh
		for x := 0; x < nw; x++ {
			sx := b.Min.X + x*w/nw
			dst.Set(x, y, img.At(sx, sy))
		}
	}
	return dst
}
