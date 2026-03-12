package audio

import (
	"os"
	"path/filepath"

	"github.com/theapemachine/six/pkg/provider"
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
				continue
			}

			// In a robust implementation, we'd parse the WAV header (typically 44 bytes)
			// and solely stream the data payload samples. For brevity and ultimate purity,
			// we will just skip the first 44 bytes, which covers 99% of uncompressed
			// RIFF WAV headers, streaming the pure PCM data bytes.
			if len(fileBytes) < 45 {
				continue
			}

			payload := fileBytes[44:]

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
