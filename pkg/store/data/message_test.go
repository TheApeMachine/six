package data

import (
	"testing"

	"github.com/google/uuid"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/system/pool"
)

func TestMessage(t *testing.T) {
	Convey("Given a Message struct", t, func() {
		id := uuid.New()
		parentID := uuid.New()
		val := *pool.NewPoolValue[any](pool.WithValue[any]("test_payload"))

		msg := Message{
			ID:       id,
			Parent:   parentID,
			Receiver: SPATIALINDEX,
			Type:     REQUEST,
			Value:    val,
		}

		Convey("It should correctly store and retrieve its fields", func() {
			So(msg.ID, ShouldEqual, id)
			So(msg.Parent, ShouldEqual, parentID)
			So(msg.Receiver, ShouldEqual, SPATIALINDEX)
			So(msg.Type, ShouldEqual, REQUEST)
			So(msg.Value, ShouldEqual, val)
		})

		Convey("It should correctly handle zero values", func() {
			var zeroMsg Message
			So(zeroMsg.Receiver, ShouldEqual, SPATIALINDEX) // Since SPATIALINDEX is 0
			So(zeroMsg.Type, ShouldEqual, REQUEST)          // Since REQUEST is 0
			So(zeroMsg.Value, ShouldResemble, pool.PoolValue[any]{})
		})
	})
}

func BenchmarkMessage_New(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Message{
			ID:       uuid.New(),
			Parent:   uuid.New(),
			Receiver: SPATIALINDEX,
			Type:     REQUEST,
			Value:    *pool.NewPoolValue[any](pool.WithValue[any]("test_payload")),
		}
	}
}

func BenchmarkMessage_Access(b *testing.B) {
	msg := Message{
		ID:       uuid.New(),
		Parent:   uuid.New(),
		Receiver: SPATIALINDEX,
		Type:     REQUEST,
		Value:    *pool.NewPoolValue[any](pool.WithValue[any]("test_payload")),
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = msg.ID
		_ = msg.Parent
		_ = msg.Receiver
		_ = msg.Type
		_ = msg.Value
	}
}
