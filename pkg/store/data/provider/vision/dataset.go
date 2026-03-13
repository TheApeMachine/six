package vision

import (
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"

	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/console"
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
	filepath.Walk(dir, func(filePath string, fileInfo os.FileInfo, entryErr error) error {
		if entryErr == nil && !fileInfo.IsDir() {
			paths = append(paths, filePath)
		}
		if entryErr != nil {
			console.Error(entryErr, "path", filePath)
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

		for idx, path := range d.paths {
			file, err := os.Open(path)
			if err != nil {
				console.Error(err, "path", path, "idx", idx)
				continue
			}

			img, _, err := image.Decode(file)
			file.Close()
			if err != nil {
				console.Error(err, "path", path, "idx", idx)
				continue
			}

			bounds := img.Bounds()
			var pos uint32 = 0

			// Stream raw RGB bytes natively into the 1D sensory timeline
			for py := bounds.Min.Y; py < bounds.Max.Y; py++ {
				for px := bounds.Min.X; px < bounds.Max.X; px++ {
					// RGBA() returns 16-bit premultiplied colors, shift to 8-bit
					red, green, blue, _ := img.At(px, py).RGBA()

					out <- provider.RawToken{SampleID: uint32(idx), Symbol: byte(red >> 8), Pos: pos}
					out <- provider.RawToken{SampleID: uint32(idx), Symbol: byte(green >> 8), Pos: pos + 1}
					out <- provider.RawToken{SampleID: uint32(idx), Symbol: byte(blue >> 8), Pos: pos + 2}

					// We skip Alpha channel for simplicity directly returning RGB bytes
					pos += 3
				}
			}
		}
	}()

	return out
}
