package vision

import (
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"

	"github.com/theapemachine/six/pkg/provider"
)

/*
Dataset walks a directory, decodes images (JPEG/PNG), and streams RGB bytes
in row-major order. Unfolds 2D to 1D; no patches, convolutions, or transforms.
Skips alpha; emits three RawTokens per pixel (r,g,b at same Pos).
*/
type Dataset struct {
	paths []string
}

/*
NewDataset walks dir recursively, collects file paths (non-dirs), and returns a Dataset.
*/
func NewDataset(dir string) *Dataset {
	var paths []string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	return &Dataset{paths: paths}
}

/*
Generate returns a channel that emits RawTokens (SampleID, Symbol, Pos) for each RGB byte.
Closes when all images are streamed. Skips files that fail to decode.
*/
func (d *Dataset) Generate() chan provider.RawToken {
	out := make(chan provider.RawToken, 4096)

	go func() {
		defer close(out)

		for i, path := range d.paths {
			file, err := os.Open(path)
			if err != nil {
				continue
			}

			img, _, err := image.Decode(file)
			file.Close()
			if err != nil {
				continue
			}

			bounds := img.Bounds()
			var pos uint32 = 0

			// Stream raw RGB bytes natively into the 1D sensory timeline
			for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
				for x := bounds.Min.X; x < bounds.Max.X; x++ {
					// RGBA() returns 16-bit premultiplied colors, shift to 8-bit
					r, g, b, _ := img.At(x, y).RGBA()

					out <- provider.RawToken{SampleID: uint32(i), Symbol: byte(r >> 8), Pos: pos}
					out <- provider.RawToken{SampleID: uint32(i), Symbol: byte(g >> 8), Pos: pos + 1}
					out <- provider.RawToken{SampleID: uint32(i), Symbol: byte(b >> 8), Pos: pos + 2}

					// We skip Alpha channel for simplicity directly returning RGB bytes
					pos += 3
				}
			}
		}
	}()

	return out
}
