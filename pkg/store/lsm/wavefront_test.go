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

		})
	}
}

func BenchmarkWavefrontSearch(b *testing.B) {
	corpus := generateCorpus(200, 26, 99)
	idx, _ := buildIndex(corpus)
	wf := NewWavefront(idx, WavefrontWithMaxHeads(32), WavefrontWithMaxDepth(64))
	promptChord := data.BaseChord(corpus[0])

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = wf.Search(promptChord, nil, nil)
	}
}

func BenchmarkSkipIndexBuild(b *testing.B) {
	corpus := generateCorpus(1000, 26, 99)
	idx, _ := buildIndex(corpus)
	skip := NewSkipIndex(idx)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		skip.Build()
	}
}

func BenchmarkSkipSearch(b *testing.B) {
	corpus := generateCorpus(1000, 26, 99)
	idx, _ := buildIndex(corpus)
	skip := NewSkipIndex(idx)
	skip.Build()

	startKey := morton.Pack(0, corpus[0])
	calc := numeric.NewCalculus()
	startPhase := calc.Multiply(
		numeric.Phase(1),
		calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32(corpus[0])),
	)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = skip.SkipSearch(startKey, startPhase)
	}
}
