package visualizer

import (
	"fmt"
	"os"
	"time"

	"github.com/theapemachine/six/pkg/data"
	"github.com/theapemachine/six/pkg/process"
)

/*
RunAliceDemo tokenizes Alice in Wonderland and streams chord events
to the visualization server for real-time 3D display.
*/
func RunAliceDemo(server *Server, alicePath string) error {
	raw, err := os.ReadFile(alicePath)
	if err != nil {
		return fmt.Errorf("reading alice: %w", err)
	}

	seq := process.NewSequencer(nil)
	var chunk []byte
	var chunkCount, edgeCount int

	for pos, b := range raw {
		chunk = append(chunk, b)
		isBoundary, _ := seq.Analyze(pos, b)

		if isBoundary && len(chunk) >= 2 {
			chord, err := data.BuildChord(chunk)
			if err != nil {
				chunk = nil
				continue
			}

			chunkCount++

			server.EmitChord(chord, string(chunk), chunkCount)
			server.EmitBoundary(chunkCount, chord.ShannonDensity())

			// Emit LSM edges for each bigram in the chunk.
			for j := 0; j < len(chunk)-1; j++ {
				edgeCount++
				server.EmitLSMEdge(chunk[j], chunk[j+1], j, edgeCount)
			}

			time.Sleep(150 * time.Millisecond)
			chunk = nil
		}
	}

	if len(chunk) >= 2 {
		chord, _ := data.BuildChord(chunk)
		chunkCount++
		server.EmitChord(chord, string(chunk), chunkCount)
		server.EmitBoundary(chunkCount, chord.ShannonDensity())
	}

	return nil
}
