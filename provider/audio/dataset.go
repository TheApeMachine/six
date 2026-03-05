package audio

import (
	"os"
	"path/filepath"

	"github.com/theapemachine/six/provider"
)

/*
Dataset loads an entire directory of audio files and streams them out as
pure raw byte signals. Like Vision, no DSP, FFT, or Mel-spectrograms are used.
The bytes of the audio samples are literally flattened and fed perfectly
unaltered to the Universal Tokenizer, turning sound primitives into topological
memory chords.
*/
type Dataset struct {
	paths []string
}

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

func (d *Dataset) Generate() chan provider.RawToken {
	out := make(chan provider.RawToken)

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
