package store

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
)

func TestNewLSMSpatialIndex(t *testing.T) {
	Convey("Given a NewLSMSpatialIndex constructor", t, func() {
		Convey("When creating a new instance", func() {
			idx := NewLSMSpatialIndex(1.0)
			So(idx, ShouldNotBeNil)
			So(len(idx.levelsKeys), ShouldEqual, 0)
			So(idx.totalCount, ShouldEqual, 0)
			So(idx.reverse, ShouldNotBeNil)
		})
	})
}

func TestLSMSpatialIndexInsertAndLookupExhaustive(t *testing.T) {
	Convey("Given an empty LSMSpatialIndex", t, func() {
		idx := NewLSMSpatialIndex(1.0)

		Convey("When inserting massive numbers of structured keys", func() {
			// We iterate over 10 different target bytes, each with 1000 sequential positions.
			// This generates 10,000 distinct chords and thoroughly exercises LSM level merges.
			for b := 0; b < 10; b++ {
				for pos := 0; pos < 1000; pos++ {
					key := (uint64(b) << 24) | uint64(pos)
					chord := data.Chord{}
					chord.Set(b + pos + 1)
					idx.Insert(key, chord)
				}
			}

			So(idx.totalCount, ShouldEqual, 10000)

			// Exhaustively verify lookups work across varying LSM levels seamlessly
			for b := 0; b < 10; b++ {
				for pos := 0; pos < 1000; pos += 100 { // check every 100th pos
					key := (uint64(b) << 24) | uint64(pos)
					res := idx.Lookup(key)
					So(res.Bytes()[0], ShouldEqual, uint64(b+pos+1))
				}
			}

			// Non-existent lookups
			resNo := idx.Lookup((uint64(99) << 24) | 500)
			So(resNo, ShouldResemble, data.Chord{})
		})
	})
}

func TestLSMSpatialIndexQueriesExhaustive(t *testing.T) {
	Convey("Given a heavily populated LSMSpatialIndex", t, func() {
		idx := NewLSMSpatialIndex(1.0)

		for b := 0; b < 5; b++ {
			for pos := 0; pos < 2000; pos++ {
				key := (uint64(b) << 24) | uint64(pos)
				chord := data.Chord{}
				chord.Set(b + pos + 1)
				idx.Insert(key, chord)
			}
		}

		Convey("When doing exact range query boundaries", func() {
			keyLo := (uint64(1) << 24) | 500
			keyHi := (uint64(1) << 24) | 1499
			res := idx.QueryRange(keyLo, keyHi)
			// from 500 to 1499 inclusive = 1000 elements
			So(len(res), ShouldEqual, 1000)
		})

		Convey("When querying globally by byte identity", func() {
			// Since we inserted 2000 of every byte (0 to 4)...
			for b := 0; b < 5; b++ {
				resB := idx.QueryByByte(byte(b))
				So(len(resB), ShouldEqual, 2000)
			}

			// Empty byte
			resEmpty := idx.QueryByByte(99)
			So(len(resEmpty), ShouldEqual, 0)
		})

		Convey("When querying targeted spatial neighborhoods", func() {
			// For byte 2, pos 1000, radius 50. Range: [950, 1050] = 101 elements.
			keyTarget := (uint64(2) << 24) | 1000
			res := idx.QueryNeighborhood(keyTarget, 50)
			So(len(res), ShouldEqual, 101)

			// Boundary radius exceeding zero (pos 10, radius 20 -> bounds [0, 30] = 31 elements)
			keyTargetLow := (uint64(2) << 24) | 10
			resLow := idx.QueryNeighborhood(keyTargetLow, 20)
			So(len(resLow), ShouldEqual, 31)
		})
	})
}

func TestLSMSpatialIndexReverseLookupExhaustive(t *testing.T) {
	Convey("Given a heavily populated LSMSpatialIndex", t, func() {
		idx := NewLSMSpatialIndex(1.0)
		chords := make([]data.Chord, 0)

		for i := 0; i < 500; i++ {
			key := uint64(i)
			chord := data.Chord{}
			chord.Set(i + 123) // Put data in another prime block

			idx.Insert(key, chord)
			chords = append(chords, chord)
		}

		Convey("When reverse looking up many existing chords", func() {
			for i, c := range chords {
				key := idx.ReverseLookup(c)
				So(key, ShouldEqual, uint64(i))
			}
		})
	})
}
