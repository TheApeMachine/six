package integration

import (
	"context"
	"os"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/store/data/provider/local"
	"github.com/theapemachine/six/pkg/store/lsm"
	"github.com/theapemachine/six/pkg/system/process"
	"github.com/theapemachine/six/pkg/system/vm"
)

/*
followChain enumerates all state chords at a Morton key by following
the generative collision chain.
*/
func followChain(spatial *lsm.SpatialIndexServer, key uint64) []data.Chord {
	var branches []data.Chord

	entry := spatial.GetEntry(key)

	if entry.ActiveCount() == 0 {
		return branches
	}

	branches = append(branches, entry)

	current := entry
	visited := make(map[lsm.ChordKey]bool)

	for {
		chainKey := lsm.ToKey(current.Rotate3D())

		if visited[chainKey] {
			break
		}

		visited[chainKey] = true

		next, exists := spatial.GetChainEntry(chainKey)

		if !exists {
			break
		}

		branches = append(branches, next)
		current = next
	}

	return branches
}

/*
stateMatch checks if any chord in the chain has the expected state bit set.
*/
func stateMatch(branches []data.Chord, expectedState int) bool {
	for _, chord := range branches {
		if chord.Has(expectedState) {
			return true
		}
	}

	return false
}

func TestRotationCompression(t *testing.T) {
	Convey("Given Alice in Wonderland ingested by the real system", t, func() {
		text, err := os.ReadFile("../../cmd/assets/alice_in_wonderland.txt")
		So(err, ShouldBeNil)

		corpus := string(text)
		chunks := ChunkStrings(corpus)
		t.Logf("Original: %d bytes, %d chunks", len(text), len(chunks))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		spatial := lsm.NewSpatialIndexServer(lsm.WithContext(ctx))

		machine := vm.NewMachine(
			vm.MachineWithContext(ctx),
			vm.MachineWithSystems(
				spatial,
				process.NewTokenizerServer(
					process.TokenizerWithContext(ctx),
					process.TokenizerWithDataset(
						local.New(local.WithStrings([]string{corpus})),
						false,
					),
				),
			),
		)

		machine.Start()
		defer machine.Stop()

		time.Sleep(5 * time.Second)

		So(spatial.Ready(), ShouldBeTrue)

		keyCount := spatial.Count()
		storeBytes := keyCount * 35

		t.Logf("Total entries (base + chain): %d", keyCount)
		t.Logf("Store size: %d bytes", storeBytes)
		t.Logf("Original size: %d bytes", len(text))

		if storeBytes < len(text) {
			t.Logf("COMPRESSION: %.2fx (%.1f%% smaller)",
				float64(len(text))/float64(storeBytes),
				(1-float64(storeBytes)/float64(len(text)))*100)
		}

		morton := data.NewMortonCoder()

		Convey("It should reconstruct chunks by computing path state", func() {
			correctTotal := 0
			testedTotal := 0
			chunksCorrect := 0

			testCount := min(500, len(chunks))

			for chunkIdx := 0; chunkIdx < testCount; chunkIdx++ {
				chunk := chunks[chunkIdx]

				if len(chunk) < 2 {
					continue
				}

				state := 1
				reconstructed := make([]byte, 0, len(chunk))

				for seqIdx := 0; seqIdx < len(chunk); seqIdx++ {
					found := false

					for b := 0; b < 256; b++ {
						candidateState := (state*3 + b) % 257
						key := morton.Pack(uint32(seqIdx), byte(b))

						if !spatial.HasKey(key) {
							continue
						}

						branches := followChain(spatial, key)

						if stateMatch(branches, candidateState) {
							reconstructed = append(reconstructed, byte(b))
							state = candidateState
							found = true
							break
						}
					}

					if !found {
						break
					}
				}

				chunkCorrect := true

				for i := range min(len(chunk), len(reconstructed)) {
					testedTotal++

					if reconstructed[i] == chunk[i] {
						correctTotal++
					} else {
						chunkCorrect = false
					}
				}

				if len(reconstructed) != len(chunk) {
					chunkCorrect = false
				}

				if chunkCorrect {
					chunksCorrect++
				}
			}

			accuracy := float64(correctTotal) / float64(max(testedTotal, 1)) * 100
			t.Logf("Tested %d chunks: %d/%d fully correct (%.1f%%)",
				testCount, chunksCorrect, testCount,
				float64(chunksCorrect)/float64(testCount)*100)
			t.Logf("Byte accuracy: %d/%d (%.1f%%)", correctTotal, testedTotal, accuracy)

			first10 := min(10, len(chunks))

			for i := 0; i < first10; i++ {
				chunk := chunks[i]
				state := 1
				reconstructed := make([]byte, 0, len(chunk))

				for seqIdx := 0; seqIdx < len(chunk); seqIdx++ {
					for b := 0; b < 256; b++ {
						candidateState := (state*3 + b) % 257
						key := morton.Pack(uint32(seqIdx), byte(b))

						if !spatial.HasKey(key) {
							continue
						}

						branches := followChain(spatial, key)

						if stateMatch(branches, candidateState) {
							reconstructed = append(reconstructed, byte(b))
							state = candidateState
							break
						}
					}
				}

				t.Logf("  chunk[%d] orig=%q recon=%q", i, chunk, string(reconstructed))
			}

			So(accuracy, ShouldBeGreaterThan, 50.0)
		})
	})
}
