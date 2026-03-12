package data

import (
	"testing"

	"github.com/google/uuid"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pool"
)

func TestMessage(t *testing.T) {
	Convey("Given a Message struct", t, func() {
		id := uuid.New()
		parentId := uuid.New()
		val := *pool.NewPoolValue[any](pool.WithValue[any]("test_payload"))

		msg := Message{
			ID:       id,
			Parent:   parentId,
			Receiver: SPATIALINDEX,
			Type:     REQUEST,
			Value:    val,
		}

		Convey("It should correctly store and retrieve its fields", func() {
			So(msg.ID, ShouldEqual, id)
			So(msg.Parent, ShouldEqual, parentId)
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
