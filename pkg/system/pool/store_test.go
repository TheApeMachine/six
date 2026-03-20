package pool

import (
	"bytes"
	"fmt"
	"io"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestResultStoreAddRelationshipSuccess(t *testing.T) {
	Convey("Given a ResultStore", t, func() {
		rs := NewResultStore()
		defer rs.Close()

		Convey("When AddRelationship links parent to child without a cycle", func() {
			err := rs.AddRelationship("parent-a", "child-b")

			Convey("It should accept the link", func() {
				So(err, ShouldBeNil)
			})
		})
	})
}

func TestResultStoreAddRelationshipCycle(t *testing.T) {
	Convey("Given a ResultStore with an existing parent/child link", t, func() {
		rs := NewResultStore()
		defer rs.Close()

		So(rs.AddRelationship("a", "b"), ShouldBeNil)

		Convey("When linking the child back to the parent", func() {
			err := rs.AddRelationship("b", "a")

			Convey("It should reject the circular dependency", func() {
				So(err, ShouldNotBeNil)
				So(err.Error(), ShouldContainSubstring, "circular")
			})
		})
	})
}

func TestResultStoreSubscribeMissingGroup(t *testing.T) {
	Convey("Given a ResultStore with no broadcast groups", t, func() {
		rs := NewResultStore()
		defer rs.Close()

		Convey("Subscribe should return nil for unknown group IDs", func() {
			ch := rs.Subscribe("missing")
			So(ch, ShouldBeNil)
		})
	})
}

func TestResultStoreStreamReadWrite(t *testing.T) {
	Convey("Given a ResultStore stream", t, func() {
		rs := NewResultStore()
		defer rs.Close()

		Convey("When bytes are written before read starts", func() {
			payload := []byte("stream-payload")
			n, werr := rs.Write(payload)
			So(werr, ShouldBeNil)
			So(n, ShouldEqual, len(payload))

			buf := make([]byte, len(payload))
			n, rerr := rs.Read(buf)
			So(rerr, ShouldBeNil)
			So(n, ShouldEqual, len(payload))
			So(string(buf), ShouldEqual, string(payload))
		})
	})
}

func TestResultStoreCloseUnblocksStreamRead(t *testing.T) {
	Convey("Given a blocked stream reader", t, func() {
		rs := NewResultStore()

		errCh := make(chan error, 1)
		go func() {
			buf := make([]byte, 16)
			_, err := rs.Read(buf)
			errCh <- err
		}()

		time.Sleep(50 * time.Millisecond)

		Convey("When the store is closed", func() {
			closeErr := rs.Close()
			So(closeErr, ShouldBeNil)

			Convey("Read should return EOF", func() {
				err := <-errCh
				So(err, ShouldEqual, io.EOF)
			})
		})
	})
}

func TestResultStoreWriteAfterClose(t *testing.T) {
	Convey("Given a closed ResultStore", t, func() {
		rs := NewResultStore()
		So(rs.Close(), ShouldBeNil)

		Convey("Write should fail with errResultStoreClosed", func() {
			_, err := rs.Write([]byte("x"))
			So(err, ShouldEqual, errResultStoreClosed)
		})
	})
}

func TestResultStoreStoreAndResult(t *testing.T) {
	Convey("Given a ResultStore", t, func() {
		rs := NewResultStore()
		defer rs.Close()

		Convey("Store and Result should round-trip values", func() {
			rs.Store("id-1", []byte("ok"), time.Minute)
			So(rs.Exists("id-1"), ShouldBeTrue)
			got, ok := rs.Result("id-1")
			So(ok, ShouldBeTrue)
			So(got.Error, ShouldBeNil)
			So(string(got.Value.([]byte)), ShouldEqual, "ok")
		})

		Convey("StoreError should record an error result", func() {
			rs.StoreError("id-err", errResultStoreClosed, time.Minute)
			got, ok := rs.Result("id-err")
			So(ok, ShouldBeTrue)
			So(got.Error, ShouldEqual, errResultStoreClosed)
		})
	})
}

func TestResultStoreAddChildDependency(t *testing.T) {
	Convey("Given a ResultStore", t, func() {
		rs := NewResultStore()
		defer rs.Close()

		Convey("AddChildDependency should record dependency edges", func() {
			rs.AddChildDependency("dep", "job")
			rs.mu.RLock()
			kids := rs.children["dep"]
			rs.mu.RUnlock()
			So(kids, ShouldResemble, []string{"job"})
		})
	})
}

func TestResultStoreCloseIdempotent(t *testing.T) {
	Convey("Given a ResultStore", t, func() {
		rs := NewResultStore()

		Convey("Close should be safe to call more than once", func() {
			So(rs.Close(), ShouldBeNil)
			So(rs.Close(), ShouldBeNil)
		})
	})
}

func TestRemoveString(t *testing.T) {
	Convey("removeString should drop the first matching element", t, func() {
		So(removeString([]string{"a", "b", "c"}, "b"), ShouldResemble, []string{"a", "c"})
		So(removeString([]string{"x"}, "x"), ShouldResemble, []string{})
		So(removeString([]string{"y"}, "z"), ShouldResemble, []string{"y"})
	})
}

func TestResultStoreCreateBroadcastGroupReuse(t *testing.T) {
	Convey("Given a ResultStore", t, func() {
		rs := NewResultStore()
		defer rs.Close()

		g1 := rs.CreateBroadcastGroup("same", time.Minute)
		g2 := rs.CreateBroadcastGroup("same", time.Minute)

		Convey("The same group instance should be returned", func() {
			So(g1, ShouldEqual, g2)
		})
	})
}

func TestResultStoreCleanupRemovesExpiredValuesAndGroups(t *testing.T) {
	Convey("Given a ResultStore with TTL-bound values and broadcast groups", t, func() {
		rs := NewResultStore()
		defer rs.Close()

		rs.Store("parent", []byte("p"), time.Minute)
		rs.Store("child", []byte("c"), time.Millisecond)
		So(rs.AddRelationship("parent", "child"), ShouldBeNil)

		childResult, ok := rs.Result("child")
		So(ok, ShouldBeTrue)
		childResult.CreatedAt = time.Now().Add(-time.Second)

		group := rs.CreateBroadcastGroup("ephemeral", time.Millisecond)
		group.LastUsed = time.Now().Add(-time.Second)

		Convey("cleanup should remove expired entries and unlink dependencies", func() {
			rs.cleanup()

			_, childExists := rs.Result("child")
			So(childExists, ShouldBeFalse)

			rs.mu.RLock()
			children := rs.children["parent"]
			_, groupExists := rs.groups["ephemeral"]
			rs.mu.RUnlock()

			So(children, ShouldResemble, []string{})
			So(groupExists, ShouldBeFalse)
		})
	})
}

func BenchmarkResultStoreExists(b *testing.B) {
	rs := NewResultStore()
	defer rs.Close()
	rs.Store("hot-key", []byte("v"), 0)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if !rs.Exists("hot-key") {
			b.Fatal("expected key")
		}
	}
}

func BenchmarkResultStoreStore(b *testing.B) {
	rs := NewResultStore()
	defer rs.Close()
	payload := []byte("payload")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		rs.Store("k", payload, 0)
	}
}

func BenchmarkResultStoreStreamWriteRead(b *testing.B) {
	rs := NewResultStore()
	defer rs.Close()
	payload := bytes.Repeat([]byte("z"), 128)
	buf := make([]byte, len(payload))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, err := rs.Write(payload)
		if err != nil {
			b.Fatal(err)
		}
		_, err = io.ReadFull(rs, buf)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkResultStoreCleanupExpired(b *testing.B) {
	rs := NewResultStore()
	defer rs.Close()

	var seq int64
	b.ReportAllocs()
	for b.Loop() {
		seq++
		id := fmt.Sprintf("exp-%d", seq)
		rs.Store(id, []byte("v"), time.Millisecond)
		result, ok := rs.Result(id)
		if !ok {
			b.Fatalf("missing result %s", id)
		}
		result.CreatedAt = time.Now().Add(-time.Second)
		rs.cleanup()
	}
}

func BenchmarkResultStoreAddRelationship(b *testing.B) {
	rs := NewResultStore()
	defer rs.Close()
	var seq int64
	b.ReportAllocs()
	for b.Loop() {
		parentID := fmt.Sprintf("p-%d", seq)
		childID := fmt.Sprintf("c-%d", seq)
		seq++
		if err := rs.AddRelationship(parentID, childID); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkResultStoreSubscribe(b *testing.B) {
	rs := NewResultStore()
	defer rs.Close()
	rs.CreateBroadcastGroup("bench-sub", time.Minute)
	b.ReportAllocs()
	for b.Loop() {
		ch := rs.Subscribe("bench-sub")
		if ch == nil {
			b.Fatal("expected subscription channel")
		}
	}
}

func BenchmarkResultStoreStoreError(b *testing.B) {
	rs := NewResultStore()
	defer rs.Close()
	var seq int64
	b.ReportAllocs()
	for b.Loop() {
		seq++
		rs.StoreError(fmt.Sprintf("err-%d", seq), errResultStoreClosed, time.Minute)
	}
}

func BenchmarkResultStoreAddChildDependency(b *testing.B) {
	rs := NewResultStore()
	defer rs.Close()
	var seq int64
	b.ReportAllocs()
	for b.Loop() {
		seq++
		rs.AddChildDependency(fmt.Sprintf("dep-%d", seq), fmt.Sprintf("job-%d", seq))
	}
}

func BenchmarkRemoveString(b *testing.B) {
	slice := []string{"a", "b", "c", "d", "e"}
	b.ReportAllocs()
	for b.Loop() {
		_ = removeString(slice, "c")
	}
}
