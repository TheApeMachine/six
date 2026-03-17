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

			Convey("It should be retrievable via Lookup", func() {
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
			})

			Convey("It should silently drop duplicate keys reflecting collision entropy", func() {
				// We invoke a duplicate insert. It must return without error.
				err2 := client.Write(ctx, func(p SpatialIndex_write_Params) error {
					p.SetKey(keyH)
					return nil
				})
				So(err2, ShouldBeNil)
			})
		})
	})
}

func BenchmarkSpatialIndexWrite(b *testing.B) {
	ctx := context.Background()
	server := NewSpatialIndexServer(WithContext(ctx))
	client := SpatialIndex_ServerToClient(server)
	defer client.Release()

	morton := data.NewMortonCoder()
	keys := make([]uint64, 256)
	for i := range 256 {
		keys[i] = morton.Pack(uint32(i), byte(i%256))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := keys[i%256]
		_ = client.Write(ctx, func(p SpatialIndex_write_Params) error {
			p.SetKey(key)
			return nil
		})
	}
}

func BenchmarkSpatialIndexLookup(b *testing.B) {
	ctx := context.Background()
	server := NewSpatialIndexServer(WithContext(ctx))
	client := SpatialIndex_ServerToClient(server)
	defer client.Release()

	morton := data.NewMortonCoder()
	key := morton.Pack(0, 'H')
	_ = client.Write(ctx, func(p SpatialIndex_write_Params) error {
		p.SetKey(key)
		return nil
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		future, release := client.Lookup(ctx, func(p SpatialIndex_lookup_Params) error {
			list, _ := capnp.NewUInt64List(p.Segment(), 1)
			list.Set(0, key)
			p.SetKeys(list)
			return nil
		})
		_, _ = future.Struct()
		release()
	}
}
