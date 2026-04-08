package validate

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestRequire(t *testing.T) {
	Convey("Given a map of required dependencies", t, func() {
		Convey("When all values are non-nil", func() {
			objs := map[string]any{
				"pool": 1,
				"ctx":  "valid",
			}

			Convey("It should return nil", func() {
				err := Require(objs)
				So(err, ShouldBeNil)
			})
		})

		Convey("When a value is nil", func() {
			objs := map[string]any{
				"pool": 1,
				"ctx":  nil,
			}

			Convey("It should return error naming the missing field", func() {
				err := Require(objs)
				So(err, ShouldNotBeNil)
				So(err.Error(), ShouldEqual, "ctx is required")
			})
		})

		Convey("When any required field is nil", func() {
			objs := map[string]any{
				"pool": nil,
				"ctx":  "valid",
			}

			Convey("It should return error for the nil field", func() {
				err := Require(objs)
				So(err, ShouldNotBeNil)
				So(err.Error(), ShouldContainSubstring, " is required")
			})
		})

		Convey("When the map is empty", func() {
			objs := map[string]any{}

			Convey("It should return nil", func() {
				err := Require(objs)
				So(err, ShouldBeNil)
			})
		})

		Convey("When a value is an empty slice", func() {
			objs := map[string]any{
				"items": []int{},
			}

			Convey("It should return nil (empty slice is non-nil)", func() {
				err := Require(objs)
				So(err, ShouldBeNil)
			})
		})

		Convey("When a typed nil pointer is placed in any", func() {
			var handle *struct{}

			objs := map[string]any{
				"handle": handle,
			}

			Convey("It should be treated as missing", func() {
				err := Require(objs)
				So(err, ShouldNotBeNil)
				So(err.Error(), ShouldEqual, "handle is required")
			})
		})

		Convey("When a nil slice variable is required", func() {
			var buf []byte

			objs := map[string]any{
				"buf": buf,
			}

			Convey("It should be treated as missing", func() {
				err := Require(objs)
				So(err, ShouldNotBeNil)
				So(err.Error(), ShouldEqual, "buf is required")
			})
		})
	})
}

func BenchmarkRequire(b *testing.B) {
	objs := map[string]any{
		"pool":         1,
		"subscription": 2,
		"groups":       3,
		"ctx":          "valid",
	}

	for b.Loop() {
		_ = Require(objs)
	}
}

func BenchmarkRequireWithNil(b *testing.B) {
	objs := map[string]any{
		"pool":  1,
		"ctx":   nil,
		"other": 3,
	}

	for b.Loop() {
		_ = Require(objs)
	}
}

func TestRequireChainLinkage(t *testing.T) {
	Convey("Given RequireChainLinkage", t, func() {
		Convey("When value is nil", func() {
			Convey("It should return ErrValueNil", func() {
				So(RequireChainLinkage(nil), ShouldEqual, ErrValueNil)
			})
		})

		Convey("When ID is zero", func() {
			var blank primitive.Value

			Convey("It should return ErrValueIDUnset", func() {
				So(RequireChainLinkage(&blank), ShouldEqual, ErrValueIDUnset)
			})
		})

		Convey("When ID is set but Prev and Next are both zero", func() {
			value, err := primitive.NewValue([]byte("payload"))
			So(err, ShouldBeNil)
			defer func() { _ = value.Close() }()

			Convey("It should return ErrValueChainUnlinked", func() {
				So(RequireChainLinkage(value), ShouldEqual, ErrValueChainUnlinked)
			})
		})

		Convey("When Prev is non-zero", func() {
			value, err := primitive.NewValue([]byte("payload"))
			So(err, ShouldBeNil)
			defer func() { _ = value.Close() }()

			value.Set(core.Cfg.Value.Region.Prev.Start, 4242)

			Convey("It should return nil", func() {
				So(RequireChainLinkage(value), ShouldBeNil)
			})
		})

		Convey("When Next is non-zero", func() {
			value, err := primitive.NewValue([]byte("payload"))
			So(err, ShouldBeNil)
			defer func() { _ = value.Close() }()

			value.Set(core.Cfg.Value.Region.Next.Start, 4243)

			Convey("It should return nil", func() {
				So(RequireChainLinkage(value), ShouldBeNil)
			})
		})
	})
}

func BenchmarkRequireChainLinkagePass(b *testing.B) {
	value, err := primitive.NewValue([]byte("payload"))
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = value.Close() }()

	value.Set(core.Cfg.Value.Region.Prev.Start, 1)

	b.ResetTimer()

	for b.Loop() {
		_ = RequireChainLinkage(value)
	}
}

func BenchmarkRequireChainLinkageRejectUnlinked(b *testing.B) {
	value, err := primitive.NewValue([]byte("payload"))
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = value.Close() }()

	b.ResetTimer()

	for b.Loop() {
		_ = RequireChainLinkage(value)
	}
}
