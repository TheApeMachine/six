package gossip

import (
	"context"
	"io"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
)

func affinityFrame(words [5]uint64) []byte {
	value := new(primitive.Value)

	for idx, word := range words {
		(*value)[123+idx] = word
	}

	return unsafe.Slice((*byte)(unsafe.Pointer(&(*value)[0])), connFrameSize)
}

func TestPriorityRouteWrite(t *testing.T) {
	Convey("Given a PriorityRoute with two Conn peers", t, func() {
		peerA := NewConn(context.Background())
		peerB := NewConn(context.Background())

		route := PriorityRoute{
			{dst: peerA, affinity: [5]uint64{}, score: 0.8},
			{dst: peerB, affinity: [5]uint64{}, score: 0.5},
		}

		frame := makeFrame(0x99)

		Convey("Write should deliver the frame to all peers", func() {
			n, err := route.Write(frame)

			So(err, ShouldBeNil)
			So(n, ShouldEqual, connFrameSize)

			bufA := make([]byte, connFrameSize)
			bufB := make([]byte, connFrameSize)

			nA, errA := peerA.Read(bufA)
			nB, errB := peerB.Read(bufB)

			So(errA, ShouldBeNil)
			So(errB, ShouldBeNil)
			So(nA, ShouldEqual, connFrameSize)
			So(nB, ShouldEqual, connFrameSize)
		})

		Convey("Write should nudge scores upward for peers that accepted the frame", func() {
			initialA := route[0].score
			initialB := route[1].score

			route.Write(frame) //nolint:errcheck

			So(route[0].score, ShouldBeGreaterThan, initialA)
			So(route[1].score, ShouldBeGreaterThan, initialB)
		})
	})

	Convey("Given an empty PriorityRoute", t, func() {
		var route PriorityRoute

		Convey("Write on an empty route should return 0 and no error", func() {
			n, err := route.Write(makeFrame(0))

			So(n, ShouldEqual, 0)
			So(err, ShouldBeNil)
		})
	})
}

func TestPriorityRouteRead(t *testing.T) {
	Convey("Given a PriorityRoute", t, func() {
		route := PriorityRoute{{dst: NewConn(context.Background())}}

		Convey("Read should return ErrClosedPipe because PriorityRoute is outbound-only", func() {
			n, err := route.Read(make([]byte, connFrameSize))

			So(err, ShouldEqual, io.ErrClosedPipe)
			So(n, ShouldEqual, 0)
		})
	})
}

func TestPriorityRouteReorder(t *testing.T) {
	Convey("Given a PriorityRoute with peers at varying score levels", t, func() {
		peerHigh := NewConn(context.Background())
		peerMid := NewConn(context.Background())
		peerLow := NewConn(context.Background())
		peerDead := NewConn(context.Background())

		route := PriorityRoute{
			{dst: peerMid, score: 0.5},
			{dst: peerLow, score: 0.1},
			{dst: peerHigh, score: 0.9},
			{dst: peerDead, score: 0.01},
		}

		Convey("Reorder should sort peers descending by score", func() {
			route.Reorder()

			So(route[0].dst, ShouldEqual, peerHigh)
			So(route[1].dst, ShouldEqual, peerMid)
			So(route[2].dst, ShouldEqual, peerLow)
		})

		Convey("Reorder should prune peers whose score is below scorePruneFloor", func() {
			route.Reorder()

			for _, peer := range route {
				So(peer.score, ShouldBeGreaterThanOrEqualTo, scorePruneFloor)
			}
		})
	})

	Convey("Given a PriorityRoute with all zero-score peers", t, func() {
		route := PriorityRoute{
			{dst: NewConn(context.Background()), score: 0},
			{dst: NewConn(context.Background()), score: 0},
		}

		Convey("Reorder should keep zero-score peers (they have not yet been evaluated)", func() {
			route.Reorder()

			So(len(route), ShouldEqual, 2)
		})
	})
}

func TestPriorityRouteClose(t *testing.T) {
	Convey("Given a PriorityRoute with peers", t, func() {
		peerA := NewConn(context.Background())
		peerB := NewConn(context.Background())

		route := PriorityRoute{
			{dst: peerA},
			{dst: peerB},
		}

		Convey("Close should close all peers without error", func() {
			So(route.Close(), ShouldBeNil)
		})
	})
}

func TestAffinityFilterWrite(t *testing.T) {
	Convey("Given an AffinityFilter targeting a specific affinity", t, func() {
		target := [5]uint64{0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
		budget := 10

		sink := NewConn(context.Background())
		filter := NewAffinityFilter(sink, target, budget)

		Convey("Write should forward frames whose affinity is within the Hamming budget", func() {
			matchingFrame := affinityFrame(target)
			n, err := filter.Write(matchingFrame)

			So(err, ShouldBeNil)
			So(n, ShouldEqual, connFrameSize)

			buf := make([]byte, connFrameSize)
			readN, readErr := sink.Read(buf)

			So(readErr, ShouldBeNil)
			So(readN, ShouldEqual, connFrameSize)
		})

		Convey("Write should drop frames whose affinity exceeds the Hamming budget", func() {
			distantAffinity := [5]uint64{0, 0, 0, 0, 0}
			distantFrame := affinityFrame(distantAffinity)

			n, err := filter.Write(distantFrame)

			So(err, ShouldBeNil)
			So(n, ShouldEqual, connFrameSize)

			buf := make([]byte, connFrameSize)
			_, readErr := sink.Read(buf)

			So(readErr, ShouldEqual, io.EOF)
		})

		Convey("Write should return ErrShortBuffer for undersized frames", func() {
			n, err := filter.Write([]byte{0x01})

			So(err, ShouldEqual, io.ErrShortBuffer)
			So(n, ShouldEqual, 0)
		})
	})
}

func BenchmarkPriorityRouteWrite(b *testing.B) {
	peers := make([]*Conn, 4)

	for idx := range peers {
		peers[idx] = NewConn(context.Background())
	}

	route := make(PriorityRoute, len(peers))

	for idx, peer := range peers {
		route[idx] = ScoredPeer{dst: peer, score: float64(idx) * 0.25}
	}

	frame := makeFrame(1)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		route.Write(frame) //nolint:errcheck

		for _, peer := range peers {
			peer.intake.Pop()
		}
	}
}

func BenchmarkAffinityFilterWrite(b *testing.B) {
	target := [5]uint64{0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	sink := NewConn(context.Background())
	filter := NewAffinityFilter(sink, target, 10)

	matchingFrame := affinityFrame(target)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		filter.Write(matchingFrame) //nolint:errcheck
		sink.intake.Pop()
	}
}

func BenchmarkAffinityFilterDrop(b *testing.B) {
	target := [5]uint64{0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	sink := NewConn(context.Background())
	filter := NewAffinityFilter(sink, target, 10)

	distantFrame := affinityFrame([5]uint64{})

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		filter.Write(distantFrame) //nolint:errcheck
	}
}
