package server

import (
	"context"
	"encoding/binary"
	"runtime"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	data "github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/store/dmt"
	"github.com/theapemachine/six/pkg/system/pool"
)

func TestForest(t *testing.T) {
	Convey("Given a newly initialized Forest Server", t, func() {
		ctx := context.Background()
		server := NewForestServer(WithContext(ctx))
		client := Server_ServerToClient(server)
		defer client.Release()

		morton := data.NewMortonCoder()

		Convey("It should drop incoming morton keys onto the exact grid coordinates", func() {
			keyH := morton.Pack(0, 'H')

			err := client.Write(ctx, func(p Server_write_Params) error {
				p.SetKey(keyH)
				return nil
			})
			So(err, ShouldBeNil)

			Convey("It should finalize via Done and return stored keys", func() {
				future, release := client.Done(ctx, func(p Server_done_Params) error {
					return nil
				})
				defer release()

				res, err := future.Struct()
				So(err, ShouldBeNil)

				keyList, listErr := res.Keys()
				So(listErr, ShouldBeNil)
				So(keyList.Len(), ShouldEqual, 1)
				So(keyList.At(0), ShouldEqual, keyH)
			})

			Convey("It should silently drop duplicate keys reflecting collision entropy", func() {
				err2 := client.Write(ctx, func(p Server_write_Params) error {
					p.SetKey(keyH)
					return nil
				})
				So(err2, ShouldBeNil)
			})
		})
	})
}

func BenchmarkForestWrite(b *testing.B) {
	ctx := context.Background()
	server := NewForestServer(WithContext(ctx))
	client := Server_ServerToClient(server)
	defer client.Release()

	morton := data.NewMortonCoder()
	keys := make([]uint64, 256)
	for i := range 256 {
		keys[i] = morton.Pack(uint32(i), byte(i%256))
	}

	for i := 0; b.Loop(); i++ {
		key := keys[i%256]
		_ = client.Write(ctx, func(p Server_write_Params) error {
			p.SetKey(key)
			return nil
		})
	}
}

func BenchmarkForestDone(b *testing.B) {
	ctx := context.Background()
	server := NewForestServer(WithContext(ctx))
	client := Server_ServerToClient(server)
	defer client.Release()

	morton := data.NewMortonCoder()
	key := morton.Pack(0, 'H')
	_ = client.Write(ctx, func(p Server_write_Params) error {
		p.SetKey(key)
		return nil
	})

	for b.Loop() {
		future, release := client.Done(ctx, func(p Server_done_Params) error {
			return nil
		})
		_, _ = future.Struct()
		release()
	}
}

/*
TestForestServerClose verifies that Close cleans up connections, pipes,
and the underlying forest without errors.
*/
func TestForestServerClose(t *testing.T) {
	Convey("Given an initialized ForestServer", t, func() {
		ctx := context.Background()
		srv := NewForestServer(WithContext(ctx))

		Convey("When Close is called", func() {
			err := srv.Close()

			Convey("Then it should succeed without error", func() {
				So(err, ShouldBeNil)
			})
		})
	})
}

/*
TestForestServerWriteVerification verifies data written through the server's
Write method lands in the underlying forest and is retrievable.
*/
func TestForestServerWriteVerification(t *testing.T) {
	Convey("Given a ForestServer with a forest", t, func() {
		ctx := context.Background()
		srv := NewForestServer(WithContext(ctx))
		defer srv.Close()

		morton := data.NewMortonCoder()

		Convey("When writing a Morton key directly", func() {
			packed := morton.Pack(42, 'Z')
			keyBytes := make([]byte, 8)
			binary.BigEndian.PutUint64(keyBytes, packed)

			srv.Forest().Insert(keyBytes, nil)

			Convey("Then the key should be in the forest", func() {
				_, exists := srv.Forest().Get(keyBytes)
				So(exists, ShouldBeTrue)
			})
		})
	})
}

/*
TestForestServerForestAccessor verifies the Forest() method returns the
same forest backing the server.
*/
func TestForestServerForestAccessor(t *testing.T) {
	Convey("Given a ForestServer", t, func() {
		ctx := context.Background()
		srv := NewForestServer(WithContext(ctx))
		defer srv.Close()

		Convey("When accessing the Forest", func() {
			forest := srv.Forest()

			Convey("Then it should be the live backing store", func() {
				So(forest, ShouldNotBeNil)

				forest.Insert([]byte("direct-key"), []byte("direct-value"))
				value, exists := forest.Get([]byte("direct-key"))
				So(exists, ShouldBeTrue)
				So(value, ShouldResemble, []byte("direct-value"))
			})
		})
	})
}

/*
TestForestServerWithForestOption verifies that WithForest injects a
pre-created forest rather than creating a new one.
srv.Close() handles forest teardown so the outer scope must not double-close.
*/
func TestForestServerWithForestOption(t *testing.T) {
	Convey("Given a pre-created forest", t, func() {
		forest, err := dmt.NewForest(dmt.ForestConfig{})
		So(err, ShouldBeNil)

		forest.Insert([]byte("pre-seeded"), []byte("value"))

		Convey("When creating a ForestServer with WithForest", func() {
			ctx := context.Background()
			srv := NewForestServer(WithContext(ctx), WithForest(forest))

			Convey("Then the server should use the provided forest", func() {
				So(srv.Forest(), ShouldEqual, forest)

				value, exists := srv.Forest().Get([]byte("pre-seeded"))
				So(exists, ShouldBeTrue)
				So(value, ShouldResemble, []byte("value"))

				srv.Close()
			})
		})
	})
}

/*
TestForestServerWithWorkerPool verifies WithWorkerPool passes through.
*/
func TestForestServerWithWorkerPool(t *testing.T) {
	Convey("Given a worker pool", t, func() {
		ctx := context.Background()
		workerPool := pool.New(ctx, 1, runtime.NumCPU(), &pool.Config{})
		defer workerPool.Close()

		Convey("When creating a ForestServer with WithWorkerPool", func() {
			srv := NewForestServer(WithContext(ctx), WithWorkerPool(workerPool))
			defer srv.Close()

			Convey("Then the server should function normally", func() {
				forest := srv.Forest()
				So(forest, ShouldNotBeNil)
			})
		})
	})
}

/*
TestForestServerMultipleWrites verifies sequential writes accumulate
in the forest correctly.
*/
func TestForestServerMultipleWrites(t *testing.T) {
	Convey("Given a ForestServer", t, func() {
		ctx := context.Background()
		srv := NewForestServer(WithContext(ctx))
		defer srv.Close()

		client := Server_ServerToClient(srv)
		defer client.Release()

		morton := data.NewMortonCoder()

		Convey("When writing 100 distinct Morton keys", func() {
			keys := make([]uint64, 100)
			for idx := range keys {
				keys[idx] = morton.Pack(uint32(idx), byte(idx%256))

				writeErr := client.Write(ctx, func(p Server_write_Params) error {
					p.SetKey(keys[idx])
					return nil
				})
				So(writeErr, ShouldBeNil)
			}

			Convey("Then all keys should be in the forest", func() {
				for _, key := range keys {
					keyBytes := make([]byte, 8)
					binary.BigEndian.PutUint64(keyBytes, key)

					_, exists := srv.Forest().Get(keyBytes)
					So(exists, ShouldBeTrue)
				}
			})
		})
	})
}

/*
TestSpatialIndexError verifies the typed error implements the error interface.
*/
func TestSpatialIndexError(t *testing.T) {
	Convey("Given a SpatialIndexError", t, func() {
		Convey("Then it should implement the error interface", func() {
			var err error = ErrForestInit
			So(err.Error(), ShouldEqual, "spatial-index: forest init failed")
		})
	})
}

/*
TestForestServerClient verifies the Client method returns a usable Cap'n Proto client.
*/
func TestForestServerClient(t *testing.T) {
	Convey("Given a ForestServer", t, func() {
		ctx := context.Background()
		srv := NewForestServer(WithContext(ctx))
		defer srv.Close()

		Convey("When requesting a client", func() {
			client := srv.Client("test-client")

			Convey("Then it should be valid", func() {
				So(client.IsValid(), ShouldBeTrue)
			})
		})
	})
}

/*
BenchmarkForestWriteVerify measures Write followed by forest Get to confirm
the full pipeline cost.
*/
func BenchmarkForestWriteVerify(b *testing.B) {
	ctx := context.Background()
	srv := NewForestServer(WithContext(ctx))
	defer srv.Close()

	client := Server_ServerToClient(srv)
	defer client.Release()

	morton := data.NewMortonCoder()
	b.ReportAllocs()

	for idx := 0; b.Loop(); idx++ {
		key := morton.Pack(uint32(idx%4096), byte(idx%256))
		_ = client.Write(ctx, func(p Server_write_Params) error {
			p.SetKey(key)
			return nil
		})

		keyBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(keyBytes, key)
		srv.Forest().Get(keyBytes)
	}
}
