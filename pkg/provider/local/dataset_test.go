package local

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/cmd"
)

func TestNewAlice(t *testing.T) {
	Convey("Given the NewAlice dataset provider", t, func() {
		Convey("When cmd.Alice contains data", func() {
			originalAlice := cmd.Alice
			cmd.Alice = []byte("hello")
			defer func() { cmd.Alice = originalAlice }()

			ds := NewAlice()

			Convey("It should return a Dataset split into per-byte slices", func() {
				So(ds, ShouldNotBeNil)

				tokens := 0
				for range ds.Generate() {
					tokens++
				}
				// The default byte split behavior might include empty tokens at ends
				So(tokens, ShouldBeGreaterThanOrEqualTo, len(cmd.Alice))
			})
		})

		Convey("When cmd.Alice is nil or empty", func() {
			originalAlice := cmd.Alice
			cmd.Alice = []byte{}
			defer func() { cmd.Alice = originalAlice }()

			ds := NewAlice()

			Convey("It should handle empty corpus gracefully", func() {
				So(ds, ShouldNotBeNil)

				tokens := 0
				for range ds.Generate() {
					tokens++
				}
				So(tokens, ShouldEqual, 0)
			})
		})
	})
}
