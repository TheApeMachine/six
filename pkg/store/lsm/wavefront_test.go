package lsm

import (
	"math"
	"math/rand"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

/*
buildIndex populates a spatial index from raw bytes using the same
GF(257) state path as the real tokenizer. Returns the index and the
per-position state trace for downstream exact validation.
*/
func buildIndex(input []byte) (*SpatialIndexServer, []numeric.Phase) {
	idx := NewSpatialIndexServer()
	calc := numeric.NewCalculus()
	state := numeric.Phase(1)

	states := make([]numeric.Phase, len(input))

	for i, b := range input {
		state = calc.Multiply(state, calc.Power(3, uint32(b)))
		states[i] = state

		baseChord := data.BaseChord(b)
		baseChord.Set(int(state))

		key := morton.Pack(uint32(i), b)
		idx.insertSync(key, baseChord, data.MustNewChord())
	}

	return idx, states
}

/*
generateCorpus creates a corpus of n bytes with controlled entropy.
*/
func generateCorpus(n int, alphabetSize int, seed int64) []byte {
	rng := rand.New(rand.NewSource(seed))
	buf := make([]byte, n)

	for i := range buf {
		buf[i] = byte(rng.Intn(alphabetSize))
	}

	return buf
}

/*
computeExpectedPhase manually runs the GF(257) state path for a byte
sequence starting from Phase(1), returning every intermediate phase.
This is the ground truth the wavefront results must match.
*/
func computeExpectedPhase(input []byte) []numeric.Phase {
	calc := numeric.NewCalculus()
	state := numeric.Phase(1)
	phases := make([]numeric.Phase, len(input))

	for i, b := range input {
		state = calc.Multiply(state, calc.Power(3, uint32(b)))
		phases[i] = state
	}

	return phases
}

func TestWavefront(t *testing.T) {
	pngHeaderA := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x01, 0x00}
	pngHeaderB := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x02, 0x00}

	gradientH := make([]byte, 64)
	gradientV := make([]byte, 64)

	for i := range gradientH {
		gradientH[i] = byte(i * 4)
		gradientV[i] = byte((i / 8) * 32)
	}

	sineA := make([]byte, 64)
	sineB := make([]byte, 64)

	for i := range sineA {
		sineA[i] = byte(128 + int(127.0*math.Sin(float64(i)*0.3)))
		sineB[i] = byte(128 + int(127.0*math.Sin(float64(i)*0.7)))
	}

	cases := map[string]map[string][]byte{
		"shared_prefix": {
			"sample1": []byte("the cat sat on the mat"),
			"sample2": []byte("the dog sat on the rug"),
		},
		"no_common_prefix": {
			"sample1": []byte("alpha beta gamma"),
			"sample2": []byte("delta epsilon zeta"),
		},
		"repeated_bytes": {
			"sample1": []byte("aaabbbcccdddeee"),
			"sample2": []byte("aaabbbfffggghh"),
		},
		"single_char_alphabet": {
			"sample1": []byte("aaaaaaaaaa"),
			"sample2": []byte("aaaaaaaaaaaaa"),
		},
		"long_diverge_late": {
			"sample1": []byte("the quick brown fox jumps over the lazy dog"),
			"sample2": []byte("the quick brown fox leaps over the lazy cat"),
		},
		"png_headers": {
			"sample1": pngHeaderA,
			"sample2": pngHeaderB,
		},
		"pixel_gradients": {
			"sample1": gradientH,
			"sample2": gradientV,
		},
		"audio_pcm_sine": {
			"sample1": sineA,
			"sample2": sineB,
		},
		"high_entropy": {
			"sample1": generateCorpus(100, 256, 1),
			"sample2": generateCorpus(100, 256, 2),
		},
	}

	calc := numeric.NewCalculus()

	for caseName, samples := range cases {
		gc.Convey("Given case: "+caseName, t, func() {
			idx := NewSpatialIndexServer()

			allStates := map[string][]numeric.Phase{}

			for sampleName, input := range samples {
				states := computeExpectedPhase(input)
				allStates[sampleName] = states

				for i, b := range input {
					baseChord := data.BaseChord(b)
					baseChord.Set(int(states[i]))

					idx.insertSync(morton.Pack(uint32(i), b), baseChord, data.MustNewChord())
				}
			}

			firstSample := samples["sample1"]
			firstStates := allStates["sample1"]

			gc.Convey(caseName+": state at position 0 should match computed phase", func() {
				key0 := morton.Pack(0, firstSample[0])
				chord := idx.GetEntry(key0)

				gc.So(chord.Has(int(firstStates[0])), gc.ShouldBeTrue)
			})

			gc.Convey(caseName+": search with first byte should find paths with valid states", func() {
				wf := NewWavefront(idx, WavefrontWithMaxHeads(64), WavefrontWithMaxDepth(uint32(len(firstSample))))
				promptChord := data.BaseChord(firstSample[0])
				results := wf.Search(promptChord, nil, nil)

				var flatStates []numeric.Phase

				for _, states := range allStates {
					flatStates = append(flatStates, states...)
				}

				for _, result := range results {
					for _, chord := range result.Path {
						matchedState := false

						for _, s := range flatStates {
							if chord.Has(int(s)) {
								matchedState = true
								break
							}
						}

						gc.So(matchedState, gc.ShouldBeTrue)
					}
				}
			})

			gc.Convey(caseName+": every result phase should be valid GF(257)", func() {
				wf := NewWavefront(idx, WavefrontWithMaxHeads(64), WavefrontWithMaxDepth(uint32(len(firstSample))))
				promptChord := data.BaseChord(firstSample[0])
				results := wf.Search(promptChord, nil, nil)

				for _, result := range results {
					gc.So(uint32(result.Phase), gc.ShouldBeGreaterThan, 0)
					gc.So(uint32(result.Phase), gc.ShouldBeLessThan, numeric.FermatPrime)
				}
			})

			gc.Convey(caseName+": result depth chord should match a ground-truth state at that depth", func() {
				wf := NewWavefront(idx, WavefrontWithMaxHeads(64), WavefrontWithMaxDepth(uint32(len(firstSample))))
				promptChord := data.BaseChord(firstSample[0])
				results := wf.Search(promptChord, nil, nil)

				for _, result := range results {
					depth := int(result.Depth)
					lastChord := result.Path[len(result.Path)-1]
					matched := false

					for _, states := range allStates {
						if depth < len(states) && lastChord.Has(int(states[depth])) {
							matched = true
							break
						}
					}

					gc.So(matched, gc.ShouldBeTrue)
				}
			})

			gc.Convey(caseName+": energy should equal cumulative BaseChord XOR popcount vs prompt", func() {
				wf := NewWavefront(idx, WavefrontWithMaxHeads(64), WavefrontWithMaxDepth(uint32(len(firstSample))))
				promptChord := data.BaseChord(firstSample[0])
				results := wf.Search(promptChord, nil, nil)

				for _, result := range results {
					minEnergy := 0
					maxEnergy := 0

					for depth, chord := range result.Path {
						stepMin := math.MaxInt
						stepMax := 0

						for name, states := range allStates {
							if depth < len(states) && chord.Has(int(states[depth])) {
								sym := samples[name][depth]
								e := data.BaseChord(sym).XOR(promptChord).ActiveCount()

								if e < stepMin {
									stepMin = e
								}

								if e > stepMax {
									stepMax = e
								}
							}
						}

						if stepMin == math.MaxInt {
							stepMin = 0
						}

						minEnergy += stepMin
						maxEnergy += stepMax
					}

					if minEnergy == maxEnergy {
						gc.So(result.Energy, gc.ShouldEqual, minEnergy)
					} else {
						gc.So(result.Energy, gc.ShouldBeBetweenOrEqual, minEnergy, maxEnergy)
					}
				}
			})

			gc.Convey(caseName+": searching with a byte not in any sample should return nil", func() {
				present := [256]bool{}

				for _, input := range samples {
					for _, b := range input {
						present[b] = true
					}
				}

				absentByte := byte(0)
				found := false

				for b := 0; b < 256; b++ {
					if !present[b] {
						absentByte = byte(b)
						found = true
						break
					}
				}

				if found {
					wf := NewWavefront(idx, WavefrontWithMaxHeads(32), WavefrontWithMaxDepth(64))
					promptChord := data.BaseChord(absentByte)
					results := wf.Search(promptChord, nil, nil)

					gc.So(results, gc.ShouldBeNil)
				}
			})

			_ = calc
		})
	}
}

func TestSkipIndex(t *testing.T) {
	gc.Convey("Given a spatial index with 200 bytes (alphabet=26)", t, func() {
		corpus := generateCorpus(200, 26, 99)
		idx, states := buildIndex(corpus)
		calc := numeric.NewCalculus()

		gc.Convey("When building a skip index", func() {
			skip := NewSkipIndex(idx)
			skip.Build()

			gc.Convey("Every Morton key in the index should have a skip entry", func() {
				for key := range idx.entries {
					_, exists := skip.entries[key]
					gc.So(exists, gc.ShouldBeTrue)
				}
			})

			gc.Convey("Level-0 jump from position 0 should target position 1 exactly", func() {
				key0 := morton.Pack(0, corpus[0])
				targetKey, _, valid := skip.Jump(key0, SkipNext)
				gc.So(valid, gc.ShouldBeTrue)

				targetPos, targetSym := morton.Unpack(targetKey)
				gc.So(targetPos, gc.ShouldEqual, 1)
				gc.So(targetSym, gc.ShouldEqual, corpus[1])
			})

			gc.Convey("Level-2 (stride 16) from position 0 should target position 16", func() {
				key0 := morton.Pack(0, corpus[0])
				targetKey, _, valid := skip.Jump(key0, Skip16)
				gc.So(valid, gc.ShouldBeTrue)

				targetPos, targetSym := morton.Unpack(targetKey)
				gc.So(targetPos, gc.ShouldEqual, 16)
				gc.So(targetSym, gc.ShouldEqual, corpus[16])
			})

			gc.Convey("Level-3 (stride 64) from position 0 should target position 64", func() {
				key0 := morton.Pack(0, corpus[0])
				targetKey, _, valid := skip.Jump(key0, Skip64)
				gc.So(valid, gc.ShouldBeTrue)

				targetPos, targetSym := morton.Unpack(targetKey)
				gc.So(targetPos, gc.ShouldEqual, 64)
				gc.So(targetSym, gc.ShouldEqual, corpus[64])
			})

			gc.Convey("Jump to non-existent key should return invalid", func() {
				_, _, valid := skip.Jump(0xDEADBEEF, SkipNext)
				gc.So(valid, gc.ShouldBeFalse)
			})

			gc.Convey("SkipSearch path chords should match actual stored state chords", func() {
				startKey := morton.Pack(0, corpus[0])
				startPhase := calc.Multiply(
					numeric.Phase(1),
					calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32(corpus[0])),
				)

				path := skip.SkipSearch(startKey, startPhase)

				for _, chord := range path {
					found := false

					for _, s := range states {
						if chord.Has(int(s)) {
							found = true
							break
						}
					}

					gc.So(found, gc.ShouldBeTrue)
				}
			})

			gc.Convey("Validate should confirm all level-0 jumps are structurally consistent", func() {
				validated := 0
				total := 0

				for i := 0; i < len(corpus)-1; i++ {
					key := morton.Pack(uint32(i), corpus[i])
					_, _, valid := skip.Jump(key, SkipNext)

					if valid {
						total++

						if skip.Validate(key, SkipNext) {
							validated++
						}
					}
				}

				gc.So(total, gc.ShouldBeGreaterThan, 0)
				gc.So(validated, gc.ShouldEqual, total)
			})
		})
	})
}
