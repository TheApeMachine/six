package data

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewListRing(t *testing.T) {

	Convey("Given a non-positive element count", t, func() {
		Convey("NewListRing should return nil", func() {
			So(NewListRing[int](0), ShouldBeNil)
			So(NewListRing[int](-1), ShouldBeNil)
		})
	})

	Convey("Given a positive element count", t, func() {
		ring := NewListRing[string](4)
		So(ring, ShouldNotBeNil)
		So(ring.Len(), ShouldEqual, 4)
	})
}

func TestListRingNext(t *testing.T) {

	Convey("Given a three-element ListRing", t, func() {
		head := NewListRing[int](3)
		So(head, ShouldNotBeNil)

		second := head.Next()
		third := second.Next()

		Convey("Next should walk forward around the ring", func() {
			So(third.Next(), ShouldEqual, head)
		})
	})
}

func TestListRingPrev(t *testing.T) {

	Convey("Given a three-element ListRing", t, func() {
		head := NewListRing[int](3)
		So(head, ShouldNotBeNil)

		third := head.Prev()
		second := third.Prev()

		Convey("Prev should walk backward around the ring", func() {
			So(second.Prev(), ShouldEqual, head)
		})
	})
}

func TestListRingMove(t *testing.T) {

	Convey("Given a four-element ListRing", t, func() {
		head := NewListRing[int](4)
		So(head, ShouldNotBeNil)

		Convey("Move(2) should advance two steps forward", func() {
			So(head.Move(2), ShouldEqual, head.Next().Next())
		})

		Convey("Move(-1) should step backward", func() {
			So(head.Move(-1), ShouldEqual, head.Prev())
		})
	})
}

func TestListRingLink(t *testing.T) {

	Convey("Given two separate ListRings", t, func() {
		left := NewListRing[int](2)
		right := NewListRing[int](3)
		So(left.Len(), ShouldEqual, 2)
		So(right.Len(), ShouldEqual, 3)

		Convey("Link should splice right after left and preserve total count", func() {
			left.Link(right)
			So(left.Len(), ShouldEqual, 5)
		})
	})
}

func TestListRingUnlink(t *testing.T) {

	Convey("Given a five-element ListRing", t, func() {
		head := NewListRing[int](5)
		So(head, ShouldNotBeNil)

		Convey("Unlink(2) should remove a two-element subring", func() {
			removed := head.Unlink(2)
			So(removed, ShouldNotBeNil)
			So(removed.Len(), ShouldEqual, 2)
			So(head.Len(), ShouldEqual, 3)
		})
	})
}

func TestListRingLen(t *testing.T) {

	Convey("Given a nil *ListRing[int]", t, func() {
		var ring *ListRing[int]

		Convey("Len should return zero without panicking", func() {
			So(ring.Len(), ShouldEqual, 0)
		})
	})
}

func TestListRingDo(t *testing.T) {

	Convey("Given a ListRing with assigned values", t, func() {
		head := NewListRing[int](3)
		So(head, ShouldNotBeNil)

		head.Value = 1
		head.Next().Value = 2
		head.Next().Next().Value = 3

		Convey("Do should visit each Value in order", func() {
			var seen []int

			head.Do(func(value int) {
				seen = append(seen, value)
			})

			So(seen, ShouldResemble, []int{1, 2, 3})
		})
	})
}

func BenchmarkListRingNext(b *testing.B) {

	head := NewListRing[int](64)
	if head == nil {
		b.Fatal("NewListRing")
	}

	cursor := head

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		cursor = cursor.Next()
	}
}
