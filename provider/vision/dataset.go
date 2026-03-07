package vision

import (
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"

	"github.com/theapemachine/six/provider"
)

/*
Dataset loads an entire directory of images and streams them out as
pure raw byte signals. It does not use pixel patches, convolutions, or
frequency domain transforms. It simply unfolds the 2D image into a 1D sequence
of RGBA bytes. The Universal tokenizer handles the spatial/structural embedding
through overlapping Fibonacci phase windows on this raw signal.
*/
type Dataset struct {
	paths []string
}

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
