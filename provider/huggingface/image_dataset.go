package huggingface

import (
	"bytes"
	"fmt"
	"image"
	imgdraw "image/draw"
	_ "image/jpeg"
	_ "image/png"
)

// DecodeImageBytes decodes a compressed image (PNG/JPEG), normalises it
// to NRGBA, and returns the raw pixel bytes. Suitable as a Dataset transform.
func DecodeImageBytes(raw []byte) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("image decode: %w", err)
	}

	bounds := src.Bounds()
	dst := image.NewNRGBA(bounds)
	imgdraw.Draw(dst, bounds, src, bounds.Min, imgdraw.Src)

	return dst.Pix, nil
}
