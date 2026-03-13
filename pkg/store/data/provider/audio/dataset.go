package audio

import (
	"log"
	"os"
	"path/filepath"

	"github.com/theapemachine/six/pkg/store/data/provider"
)

/*
Dataset walks a directory of audio files and streams raw PCM bytes.
Assumes RIFF WAV: skips first 44 bytes (header), streams remainder as-is.
No DSP, FFT, or Mel. Emits one RawToken per sample byte.
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
		// Basic filter to ignore directories. In production, we should ensure
		// we only load .wav or applicable raw PCM files.
		if err == nil && !info.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	return &Dataset{paths: paths}
}

/*
Generate returns a channel that emits RawTokens for each PCM byte (after WAV header).
Closes when all files are streamed. Skips files shorter than 45 bytes.
*/
func (d *Dataset) Generate() chan provider.RawToken {
	out := make(chan provider.RawToken, 4096)

	go func() {
		defer close(out)

		for i, path := range d.paths {
			fileBytes, err := os.ReadFile(path)
			if err != nil {
				log.Printf("error reading %s: %v", path, err)
				continue
			}

			payloadOffset := 44 // default skip
			if len(fileBytes) >= 12 && string(fileBytes[0:4]) == "RIFF" && string(fileBytes[8:12]) == "WAVE" {
				offset := 12
				for offset+8 <= len(fileBytes) {
					chunkID := string(fileBytes[offset : offset+4])
					chunkSize := int(uint32(fileBytes[offset+4]) | uint32(fileBytes[offset+5])<<8 | uint32(fileBytes[offset+6])<<16 | uint32(fileBytes[offset+7])<<24)
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
			for _, b := range payload {
				out <- provider.RawToken{
					SampleID: uint32(i),
					Symbol:   b,
					Pos:      pos,
				}
				pos++
			}
		}
	}()

	return out
}
