package programmer

import (
	"testing"

	"github.com/theapemachine/six/pkg/primitive"

	. "github.com/smartystreets/goconvey/convey"
)

/*
TestNewAsset wires Value and Region without copying payload yet.
*/
func TestNewAsset(t *testing.T) {
	Convey("Given two distinct Values", t, func() {
		left, errLeft := primitive.NewValue([]byte("left"))
		right, errRight := primitive.NewValue([]byte("right"))

		So(errLeft, ShouldBeNil)
		So(errRight, ShouldBeNil)
		So(len(left), ShouldBeGreaterThan, 0)
		So(len(right), ShouldBeGreaterThan, 0)

		asset := NewAsset(right[0], primitive.TokenRegion)

		Convey("NewAsset should retain the source value and region", func() {
			So(asset.Value, ShouldEqual, right[0])
			So(asset.Region, ShouldEqual, primitive.TokenRegion)
		})

		Reset(func() {
			left[0].Close()
			right[0].Close()
		})
	})
}

/*
TestAsset_Bundle copies a like-sized region slice from the asset Value into the destination Value.
*/
func TestAsset_Bundle(t *testing.T) {
	Convey("Given an asset whose token region can be copied into another Value", t, func() {
		srcVals, errSrc := primitive.NewValue([]byte("bundle-src"))
		dstVals, errDst := primitive.NewValue([]byte("bundle-dst"))

		So(errSrc, ShouldBeNil)
		So(errDst, ShouldBeNil)

		src := srcVals[0]
		dst := dstVals[0]

		tokenSrc := src.Get(primitive.TokenRegion)
		tokenDst := dst.Get(primitive.TokenRegion)

		So(len(tokenSrc), ShouldEqual, len(tokenDst))
		So(len(tokenSrc), ShouldBeGreaterThan, 0)

		mark := uint64(0xDEADBEEF)
		tokenSrc[0] = mark

		asset := NewAsset(src, primitive.TokenRegion)

		Convey("Bundle should copy token words into the destination", func() {
			err := asset.Bundle(dst)

			So(err, ShouldBeNil)
			So(dst.Get(primitive.TokenRegion)[0], ShouldEqual, mark)
		})

		Reset(func() {
			src.Close()
			dst.Close()
		})
	})

	Convey("Given an asset targeting the same Value as destination", t, func() {
		values, err := primitive.NewValue([]byte("self"))

		So(err, ShouldBeNil)

		value := values[0]
		asset := NewAsset(value, primitive.TokenRegion)

		Convey("Bundle should refuse self-copy", func() {
			err := asset.Bundle(value)

			So(err, ShouldNotBeNil)
		})

		Reset(func() {
			value.Close()
		})
	})

	Convey("Given a nil Value in the asset", t, func() {
		values, err := primitive.NewValue([]byte("nil-asset"))

		So(err, ShouldBeNil)

		dst := values[0]
		asset := NewAsset(nil, primitive.TokenRegion)

		Convey("Bundle should error", func() {
			err := asset.Bundle(dst)

			So(err, ShouldNotBeNil)
		})

		Reset(func() {
			dst.Close()
		})
	})
}

func BenchmarkAsset_Bundle(b *testing.B) {
	srcVals, errSrc := primitive.NewValue([]byte("bench-asset-src"))
	dstVals, errDst := primitive.NewValue([]byte("bench-asset-dst"))

	if errSrc != nil || errDst != nil || len(srcVals) == 0 || len(dstVals) == 0 {
		b.Fatal(errSrc, errDst)
	}

	src := srcVals[0]
	dst := dstVals[0]

	defer src.Close()
	defer dst.Close()

	tokenSrc := src.Get(primitive.TokenRegion)
	tokenSrc[0] = 1

	asset := NewAsset(src, primitive.TokenRegion)

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_ = asset.Bundle(dst)
	}
}

func BenchmarkNewAsset(b *testing.B) {
	values, err := primitive.NewValue([]byte("bench new asset"))

	if err != nil || len(values) == 0 {
		b.Fatal(err)
	}

	value := values[0]
	defer value.Close()

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_ = NewAsset(value, primitive.TokenRegion)
	}
}
