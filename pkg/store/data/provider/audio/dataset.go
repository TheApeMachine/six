package audio

import (
	"context"
	"iter"
	"log"
	"math"
	"os"
	"path/filepath"

	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/store/data/provider"
)

/*
Dataset walks a directory of audio files and streams raw PCM bytes.
Assumes RIFF WAV: skips first 44 bytes (header), streams remainder as-is.
No DSP, FFT, or Mel. Emits one RawToken per sample byte.
*/
type Dataset struct {
	ctx    context.Context
	cancel context.CancelFunc
	state  *errnie.State
	dir    string
	paths  []string
}

type datasetOpts func(*Dataset)

/*
NewDataset walks dir recursively, collects file paths (non-dirs), and returns a Dataset.
*/
func NewDataset(opts ...datasetOpts) *Dataset {
	dataset := &Dataset{
		state: errnie.NewState("audio-dataset"),
		paths: []string{},
	}

	for _, opt := range opts {
		opt(dataset)
	}

	return dataset
}

/*
Generate returns an iterator of RawTokens for each PCM byte (after WAV header).
Skips files shorter than the payload offset.
*/
func (d *Dataset) Generate() iter.Seq[provider.RawToken] {
	return func(yield func(provider.RawToken) bool) {
		filepath.Walk(d.dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				log.Printf("walk %s: %v", path, err)
				return nil
			}

			if !info.IsDir() {
				d.paths = append(d.paths, path)
			}

			return nil
		})

		for fileIdx, path := range d.paths {
			fileBytes := errnie.Guard(d.state, func() ([]byte, error) {
				return os.ReadFile(path)
			})

			payloadOffset := 44 // default skip

			if len(fileBytes) >= 12 && string(fileBytes[0:4]) == "RIFF" && string(fileBytes[8:12]) == "WAVE" {
				offset := 12
				for offset+8 <= len(fileBytes) {
					chunkID := string(fileBytes[offset : offset+4])
					chunkSize := int(
						uint32(fileBytes[offset+4]) | uint32(fileBytes[offset+5])<<8 | uint32(fileBytes[offset+6])<<16 | uint32(fileBytes[offset+7])<<24,
					)

					if chunkSize < 0 || offset+8+chunkSize > len(fileBytes) {
						break
					}

					if chunkID == "data" {
						payloadOffset = offset + 8

						if payloadOffset+chunkSize > len(fileBytes) {
							chunkSize = len(fileBytes) - payloadOffset
						}

						fileBytes = fileBytes[:payloadOffset+chunkSize]
						break
					}

					offset += 8 + chunkSize
				}
			}

			if len(fileBytes) <= payloadOffset {
				continue
			}

			payload := fileBytes[payloadOffset:]

			var pos uint32 = 0

			for _, pcmByte := range payload {
				if pos == math.MaxUint32 {
					break
				}

				if !yield(provider.RawToken{
					SampleID: uint32(fileIdx),
					Symbol:   pcmByte,
					Pos:      pos,
				}) {
					return
				}

				pos++
			}
		}
	}
}

func DatasetWithContext(ctx context.Context) datasetOpts {
	return func(dataset *Dataset) {
		dataset.ctx, dataset.cancel = context.WithCancel(ctx)
	}
}

func WithDirectory(dir string) datasetOpts {
	return func(dataset *Dataset) {
		dataset.dir = dir
	}
}
