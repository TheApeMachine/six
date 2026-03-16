package numeric

import (
	"math"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
)

func TestCoreFunctions(t *testing.T) {
	Convey("Given numeric core functions", t, func() {
		Convey("PackResult and DecodePacked should round-trip correctly", func() {
			scenarios := []struct {
				score        int32
				invertedDist uint16
				id           int
			}{
				{0, 10, 5},
				{packedScoreMin, 0xFFFF, 0xFFFFFF},
				{packedScoreMax, 0, 0},
				{1000, 100, 100},
				{-1000, 50, 50},
			}

			for _, s := range scenarios {
				packed := PackResult(s.score, s.invertedDist, s.id)
				decodedID, decodedScore := DecodePacked(packed)

				So(decodedID, ShouldEqual, s.id)
				expectedScore := float64(s.score) / ScoreScale
				So(decodedScore, ShouldAlmostEqual, expectedScore)
			}
		})

		Convey("PackResult should clamp out-of-bounds scores", func() {
			packedLow := PackResult(packedScoreMin-100, 1, 1)
			_, decodedScoreLow := DecodePacked(packedLow)
			expectedScoreLow := float64(packedScoreMin) / ScoreScale
			So(decodedScoreLow, ShouldAlmostEqual, expectedScoreLow)

			packedHigh := PackResult(packedScoreMax+100, 1, 1)
			_, decodedScoreHigh := DecodePacked(packedHigh)
			expectedScoreHigh := float64(packedScoreMax) / ScoreScale
			So(decodedScoreHigh, ShouldAlmostEqual, expectedScoreHigh)
		})

		Convey("RebasePackedID should properly shift IDs and handle clamping", func() {
			packed := PackResult(0, 0, 100)
			
			// Positive shift
			rebasedPos := RebasePackedID(packed, 50)
			decodedIDPos, _ := DecodePacked(rebasedPos)
			So(decodedIDPos, ShouldEqual, 150)

			// Negative shift
			rebasedNeg := RebasePackedID(packed, -50)
			decodedIDNeg, _ := DecodePacked(rebasedNeg)
			So(decodedIDNeg, ShouldEqual, 50)

			// Negative shift with clamping to 0
			rebasedClampBottom := RebasePackedID(packed, -150)
			decodedIDClampBottom, _ := DecodePacked(rebasedClampBottom)
			So(decodedIDClampBottom, ShouldEqual, 0)
			
			// Positive shift with clamping to max24
			rebasedClampTop := RebasePackedID(packed, 0xFFFFFF)
			decodedIDClampTop, _ := DecodePacked(rebasedClampTop)
			So(decodedIDClampTop, ShouldEqual, 0xFFFFFF)
		})

		Convey("PtrToBytes should handle pointers securely", func() {
			slice, err := PtrToBytes(nil, 0)
			So(err, ShouldBeNil)
			So(len(slice), ShouldEqual, 0)

			slice, err = PtrToBytes(nil, 10)
			So(err, ShouldNotBeNil)
			So(slice, ShouldBeNil)

			data := [5]byte{1, 2, 3, 4, 5}
			slice, err = PtrToBytes(unsafe.Pointer(&data[0]), len(data))
			So(err, ShouldBeNil)
			So(len(slice), ShouldEqual, 5)
			So(slice[0], ShouldEqual, 1)
			So(slice[4], ShouldEqual, 5)
		})

		Convey("FirstPtr should retrieve pointer effectively", func() {
			ptrNone := FirstPtr(nil)
			So(ptrNone == nil, ShouldBeTrue)

			ptrEmpty := FirstPtr([]byte{})
			So(ptrEmpty == nil, ShouldBeTrue)

			data := []byte{42}
			ptrData := FirstPtr(data)
			So(ptrData, ShouldNotBeNil)
			So(*(*byte)(ptrData), ShouldEqual, 42)
		})

		Convey("AbsInt should return absolute values, bypassing constraints appropriately", func() {
			So(AbsInt(0), ShouldEqual, 0)
			So(AbsInt(42), ShouldEqual, 42)
			So(AbsInt(-42), ShouldEqual, 42)
			So(AbsInt(math.MinInt), ShouldEqual, math.MaxInt)
		})
	})
}
