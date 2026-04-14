package programmer

import (
	"testing"

	"github.com/theapemachine/six/pkg/primitive"

	. "github.com/smartystreets/goconvey/convey"
)

/*
TestNewFirmware constructs the rule-evaluator handle used by Next.
*/
func TestNewFirmware(t *testing.T) {
	Convey("When NewFirmware is called", t, func() {
		fw := NewFirmware()

		Convey("It should return a non-nil Firmware", func() {
			So(fw, ShouldNotBeNil)
		})
	})
}

/*
TestFirmware_HasBits scans uint64 words for any set bit.
*/
func TestFirmware_HasBits(t *testing.T) {
	Convey("Given a Firmware receiver", t, func() {
		var firmware Firmware

		Convey("HasBits should return false for nil or empty slices", func() {
			So(firmware.HasBits(nil), ShouldBeFalse)
			So(firmware.HasBits([]uint64{}), ShouldBeFalse)
		})

		Convey("HasBits should return true when any word is non-zero", func() {
			So(firmware.HasBits([]uint64{0, 1}), ShouldBeTrue)
		})
	})
}

/*
TestFirmware_Next selects firmware from value.rules when boolean conditions match region occupancy.
*/
func TestFirmware_Next(t *testing.T) {
	Convey("Given a fresh Value (affinity region all zero)", t, func() {
		values, err := primitive.NewValue([]byte("firmware next"))

		So(err, ShouldBeNil)
		So(len(values), ShouldBeGreaterThan, 0)

		value := values[0]
		firmware := NewFirmware()

		Convey("Next should return the rule firmware when affinity is empty and the rule expects false", func() {
			next := firmware.Next(value)

			So(next, ShouldEqual, "affinity")
		})

		Reset(func() {
			value.Close()
		})
	})
}

func BenchmarkFirmware_HasBits(b *testing.B) {
	var firmware Firmware
	region := []uint64{0, 0, 0, 1}

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_ = firmware.HasBits(region)
	}
}

func BenchmarkFirmware_Next(b *testing.B) {
	values, err := primitive.NewValue([]byte("bench firmware"))

	if err != nil || len(values) == 0 {
		b.Fatal(err)
	}

	value := values[0]
	defer value.Close()

	fw := NewFirmware()

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_ = fw.Next(value)
	}
}
