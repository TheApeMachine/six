package vision

import (
	"context"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"iter"
	"os"
	"path/filepath"

	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/console"
	"github.com/theapemachine/six/pkg/system/pool"
)

/*
Dataset walks a directory, decodes images (JPEG/PNG), and streams RGB bytes
in row-major order. Unfolds 2D to 1D; no patches, convolutions, or transforms.
Skips alpha; each pixel unfolds into three RawTokens at sequential positions
(Pos, Pos+1, Pos+2) for r, g, b.
*/
type Dataset struct {
	ctx    context.Context
	cancel context.CancelFunc
	dir    string
	paths  []string
	pool   *pool.Pool
}

type datasetOpts func(*Dataset)

/*
NewDataset walks dir recursively, collects file paths (non-dirs), and returns a Dataset.
*/
func NewDataset(opts ...datasetOpts) *Dataset {
	dataset := &Dataset{
		paths: []string{},
	}

	for _, opt := range opts {
		opt(dataset)
	}

	return dataset
}

/*
Generate returns an iterator of RawTokens for each RGB byte.
Skips files that fail to decode.
*/
func (dataset *Dataset) Generate() iter.Seq[provider.RawToken] {
	return func(yield func(provider.RawToken) bool) {
		filepath.Walk(dataset.dir, func(filePath string, fileInfo os.FileInfo, entryErr error) error {
			if entryErr == nil && !fileInfo.IsDir() {
				dataset.paths = append(dataset.paths, filePath)
			}

			if entryErr != nil {
				console.Error(entryErr, "path", filePath)
			}

			return nil
		})

		for idx, path := range dataset.paths {
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

					if !yield(provider.RawToken{
						SampleID: uint32(idx), Symbol: byte(red >> 8), Pos: pos,
					}) {
						return
					}

					if !yield(provider.RawToken{
						SampleID: uint32(idx), Symbol: byte(green >> 8), Pos: pos + 1,
					}) {
						return
					}

					if !yield(provider.RawToken{
						SampleID: uint32(idx), Symbol: byte(blue >> 8), Pos: pos + 2,
					}) {
						return
					}

					// We skip Alpha channel for simplicity directly returning RGB bytes
					pos += 3
				}
			}
		}
	}
}

func DatasetWithContext(ctx context.Context) datasetOpts {
	return func(dataset *Dataset) {
		dataset.ctx, dataset.cancel = context.WithCancel(ctx)
	}
}

func DatasetWithPool(pool *pool.Pool) datasetOpts {
	return func(dataset *Dataset) {
		dataset.pool = pool
	}
}

func WithDirectory(dir string) datasetOpts {
	return func(dataset *Dataset) {
		dataset.dir = dir
	}
}
