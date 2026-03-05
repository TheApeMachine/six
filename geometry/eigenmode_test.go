package geometry

import (
	"math"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/numeric"
)

func mockBaseChord(b byte) data.Chord {
	var chord data.Chord
	totalBits := numeric.ChordBlocks * 64

	offsets := [5]int{
		int(b) * 7,
		int(b) * 13,
		int(b) * 31,
		int(b) * 61,
		int(b) * 127,
	}

	for _, off := range offsets {
		bit := off % totalBits
		chord[bit/64] |= 1 << (bit % 64)
	}

	return chord
}

func TestNewEigenMode(t *testing.T) {
	Convey("Given NewEigenMode constructor", t, func() {
		Convey("When creating with no options", func() {
			ei := NewEigenMode()
			So(ei, ShouldNotBeNil)
			So(len(ei.PhaseTheta), ShouldEqual, 256)
			So(len(ei.PhasePhi), ShouldEqual, 256)
			So(len(ei.FreqTheta), ShouldEqual, 256)
			So(len(ei.FreqPhi), ShouldEqual, 256)
		})

		Convey("When creating with options", func() {
			opt := func(ei *EigenMode) {
				ei.FreqTheta[0] = 42.0
				ei.FreqPhi[0] = 73.0
			}
			ei := NewEigenMode(opt)
			So(ei.FreqTheta[0], ShouldEqual, 42.0)
			So(ei.FreqPhi[0], ShouldEqual, 73.0)
		})
	})
}

func TestBuildMultiScaleCooccurrence(t *testing.T) {
	Convey("Given an EigenMode and chord sequence", t, func() {
		ei := NewEigenMode()

		// Chord-native: build from chord sequence (no raw bytes)
		corpus := []byte("abababa")
		chords := make([]data.Chord, len(corpus))
		for i, b := range corpus {
			chords[i] = mockBaseChord(b)
		}

		Convey("When building multiscale cooccurrence", func() {
			err := ei.BuildMultiScaleCooccurrence(chords)
			So(err, ShouldBeNil)

			// Simple check that it modified state over zero-values
			var hasNonZeroThetaFreq, hasNonZeroPhiFreq bool
			for i := range 256 {
				if ei.FreqTheta[i] > 0.0 {
					hasNonZeroThetaFreq = true
				}
				if ei.FreqPhi[i] > 0.0 {
					hasNonZeroPhiFreq = true
				}
			}
			So(hasNonZeroThetaFreq, ShouldBeTrue)
			So(hasNonZeroPhiFreq, ShouldBeTrue)
		})

		Convey("When handling empty chords", func() {
			emptyEI := NewEigenMode()
			err := emptyEI.BuildMultiScaleCooccurrence(nil)
			So(err, ShouldBeNil)
			// Phase and Frequency should remain unmodified (zeroed)
			So(emptyEI.PhaseTheta[0], ShouldEqual, 0)
			So(emptyEI.FreqTheta[0], ShouldEqual, 0)
			So(emptyEI.PhasePhi[0], ShouldEqual, 0)
			So(emptyEI.FreqPhi[0], ShouldEqual, 0)
		})
	})
}

func TestBuildChordCooccurrenceInto(t *testing.T) {
	Convey("Given an EigenMode and a target matrix", t, func() {
		ei := NewEigenMode()
		var C [256][256]float64

		Convey("When building with chord sequence and window", func() {
			chords := []data.Chord{
				mockBaseChord('a'),
				mockBaseChord('b'),
				mockBaseChord('c'),
				mockBaseChord('d'),
			}

			ei.buildChordCooccurrenceInto(&C, chords, 2)

			// Row sums = 1 (Markov normalization)
			binA := data.ChordBin(&chords[0])
			var rowSum float64
			for j := range 256 {
				rowSum += C[binA][j]
			}
			So(rowSum, ShouldEqual, 1.0)
		})

		Convey("When chords are empty", func() {
			ei.buildChordCooccurrenceInto(&C, nil, 5)
			for i := range 256 {
				for j := range 256 {
					So(C[i][j], ShouldEqual, 0)
				}
			}
		})
	})
}

func TestToroidalEigenvectors(t *testing.T) {
	Convey("Given a populated co-occurrence matrix", t, func() {
		ei := NewEigenMode()
		var C [256][256]float64
		// Create a uniform transition matrix
		val := 1.0 / 256.0
		for i := range 256 {
			for j := range 256 {
				C[i][j] = val
			}
		}

		Convey("When extracting toroidal eigenvectors (Theta and Phi planes)", func() {
			vT1, vT2, vP1, vP2, err := ei.toroidalEigenvectors(&C)

			// Uniform matrix should successfully factorize
			So(err, ShouldBeNil)

			// Eigenvectors should have equal length 256
			So(len(vT1), ShouldEqual, 256)
			So(len(vT2), ShouldEqual, 256)
			So(len(vP1), ShouldEqual, 256)
			So(len(vP2), ShouldEqual, 256)
		})
	})
}

func TestNormalizeVec(t *testing.T) {
	Convey("Given a vector normalizer", t, func() {
		ei := NewEigenMode()

		Convey("When normalizing a non-zero vector", func() {
			var v [256]float64
			v[0] = 3.0
			v[1] = 4.0 // magnitude 5

			ei.normalizeVec(&v)

			So(v[0], ShouldEqual, 3.0/5.0)
			So(v[1], ShouldEqual, 4.0/5.0)
		})

		Convey("When handling a zero vector", func() {
			var v [256]float64
			ei.normalizeVec(&v)

			for i := range 256 {
				So(v[i], ShouldEqual, 0.0)
			}
		})
	})
}

func TestSeqToroidalMeanPhase(t *testing.T) {
	Convey("Given a populated EigenMode toroidal phase sequence", t, func() {
		ei := NewEigenMode()

		// chordA and chordB must map to different ChordBins ('a'/'b' collide)
		chordA := mockBaseChord(0)
		binA := data.ChordBin(&chordA)
		var chordB data.Chord
		for b := 1; b < 256; b++ {
			c := mockBaseChord(byte(b))
			if data.ChordBin(&c) != binA {
				chordB = c
				break
			}
		}
		binB := data.ChordBin(&chordB)

		ei.PhaseTheta[binA] = 0.0
		ei.PhaseTheta[binB] = math.Pi / 2.0 // pointing UP (sin=1, cos=0)
		ei.PhasePhi[binA] = -math.Pi / 2.0  // pointing DOWN
		ei.PhasePhi[binB] = math.Pi         // pointing LEFT

		Convey("When calculating mean phase of chord sequence", func() {
			meanTheta, meanPhi := ei.SeqToroidalMeanPhase([]data.Chord{chordA, chordB})
			So(meanTheta, ShouldAlmostEqual, math.Pi/4, 0.0001)
			So(meanPhi, ShouldAlmostEqual, -3.0*math.Pi/4.0, 0.0001)
		})

		Convey("When calculating mean phase for empty chords", func() {
			meanTheta, meanPhi := ei.SeqToroidalMeanPhase(nil)
			So(meanTheta, ShouldEqual, 0.0)
			So(meanPhi, ShouldEqual, 0.0)
		})

		Convey("When calculating mean of same-chord elements", func() {
			// chordB already has theta=π/2, phi=π
			meanTheta, meanPhi := ei.SeqToroidalMeanPhase([]data.Chord{chordB, chordB})
			So(meanTheta, ShouldAlmostEqual, math.Pi/2, 0.0001)
			So(meanPhi, ShouldAlmostEqual, math.Pi, 0.0001)
		})
	})
}
