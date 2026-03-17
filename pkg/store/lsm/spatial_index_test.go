package lsm

import (
	"context"
	"testing"

	capnp "capnproto.org/go/capnp/v3"
	. "github.com/smartystreets/goconvey/convey"
	data "github.com/theapemachine/six/pkg/store/data"
)

func TestSpatialIndex(t *testing.T) {
	Convey("Given a newly initialized SpatialIndex Server", t, func() {
		ctx := context.Background()
		server := NewSpatialIndexServer(WithContext(ctx))
		client := SpatialIndex_ServerToClient(server)
		defer client.Release()

		morton := data.NewMortonCoder()

		Convey("It should drop incoming morton keys onto the exact grid coordinates", func() {
			keyH := morton.Pack(0, 'H')

			err := client.Write(ctx, func(p SpatialIndex_write_Params) error {
				p.SetKey(keyH)
				return nil
			})
			So(err, ShouldBeNil)

			server.mu.RLock()
			_, exists := server.grid['H'][0]
			server.mu.RUnlock()
			So(exists, ShouldBeTrue)

			Convey("It should silently drop duplicate keys reflecting collision entropy", func() {
				// We invoke a duplicate insert. It must return without error.
				err2 := client.Write(ctx, func(p SpatialIndex_write_Params) error {
					p.SetKey(keyH)
					return nil
				})
				So(err2, ShouldBeNil)
			})

			Convey("It should return instantiated Values directly from the grid on Lookup", func() {
				future, release := client.Lookup(ctx, func(p SpatialIndex_lookup_Params) error {
					list, err := capnp.NewUInt64List(p.Segment(), 1)
					if err != nil {
						return err
					}
					list.Set(0, keyH)
					p.SetKeys(list)
					return nil
				})
				defer release()

				res, err := future.Struct()
				So(err, ShouldBeNil)

				values, err := res.Values()
				So(err, ShouldBeNil)
				So(values.Len(), ShouldEqual, 1)

				// The entry should be empty data.Value{} as instantiated by Write
				val := values.At(0)
				So(val.ActiveCount(), ShouldEqual, 0)
			})
		})
	})
}
