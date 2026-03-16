package huggingface

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestDecodeImageBytes(t *testing.T) {
	Convey("Given valid encoded image bytes", t, func() {
		// create a small dummy image
		img := image.NewNRGBA(image.Rect(0, 0, 10, 10))
		for y := 0; y < 10; y++ {
			for x := 0; x < 10; x++ {
				img.Set(x, y, color.NRGBA{255, 0, 0, 255})
			}
		}

		Convey("When decoding PNG bytes", func() {
			var buf bytes.Buffer
			png.Encode(&buf, img)

			pix, err := DecodeImageBytes(buf.Bytes())
			So(err, ShouldBeNil)
			So(len(pix), ShouldEqual, 10*10*4)
		})

		Convey("When decoding JPEG bytes", func() {
			var buf bytes.Buffer
			jpeg.Encode(&buf, img, nil)

			pix, err := DecodeImageBytes(buf.Bytes())
			So(err, ShouldBeNil)
			So(len(pix), ShouldEqual, 10*10*4)
		})
	})

	Convey("Given invalid/corrupted bytes", t, func() {
		Convey("It should return an error", func() {
			pix, err := DecodeImageBytes([]byte("not an image"))
			So(err, ShouldNotBeNil)
			So(pix, ShouldBeNil)
		})
	})
}


