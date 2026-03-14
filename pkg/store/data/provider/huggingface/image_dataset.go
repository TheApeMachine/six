package huggingface

import (
	"bytes"
	"fmt"
	"image"
	imgdraw "image/draw"
	_ "image/jpeg"
	_ "image/png"
)

/*
DecodeImageBytes decodes PNG/JPEG bytes to NRGBA and returns the raw pixel buffer.
Use as a Dataset transform (e.g. DatasetWithTransform) when the HuggingFace column
holds compressed image bytes.
*/
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
