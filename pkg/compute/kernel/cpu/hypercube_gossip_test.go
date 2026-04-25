package cpu

import (
	"testing"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/program"
	"github.com/theapemachine/six/pkg/primitive"

	. "github.com/smartystreets/goconvey/convey"
)

func TestHypercubeGossip(t *testing.T) {
	Convey("Given a fold topology instruction", t, func() {
		layout := program.Layout{
			Regions: map[string]program.RegionExtent{
				"tokens":  {Start: primitive.TokensStartWord, Words: primitive.TokensWords},
				"signals": {Start: primitive.SignalsStartWord, Words: primitive.SignalsWords},
			},
		}
		compiled, err := program.Compile(`[ (signals[0,1] fold) <= (tokens[0,1] | 0) <= community ]`, layout)
		So(err, ShouldBeNil)

		first := primitive.Emit()
		defer first.Close()
		second := primitive.Emit()
		defer second.Close()
		third := primitive.Emit()
		defer third.Close()

		So(first.InstallProgram(compiled.Words), ShouldBeTrue)
		So(second.InstallProgram(compiled.Words), ShouldBeTrue)
		So(third.InstallProgram(compiled.Words), ShouldBeTrue)

		first.Set(primitive.TokensStartWord, 0b001)
		second.Set(primitive.TokensStartWord, 0b010)
		third.Set(primitive.TokensStartWord, 0b100)

		HypercubeGossip(nil, []*primitive.Value{first, second, third})

		So(first.Get(primitive.SignalsRegion)[0], ShouldEqual, 0b111)
		So(second.Get(primitive.SignalsRegion)[0], ShouldEqual, 0b111)
		So(third.Get(primitive.SignalsRegion)[0], ShouldEqual, 0b111)
	})

	Convey("Given an in-band geometric instruction", t, func() {
		layout := program.Layout{
			Regions: map[string]program.RegionExtent{
				"signals":  {Start: primitive.SignalsStartWord, Words: primitive.SignalsWords},
				"context":  {Start: primitive.ContextStartWord, Words: primitive.ContextWords},
				"gradient": {Start: primitive.GradientStartWord, Words: primitive.GradientWords},
			},
		}
		compiled, err := program.Compile(`[ (signals self) <= (context compose gradient) <= community ]`, layout)
		So(err, ShouldBeNil)

		actual := primitive.Emit()
		defer actual.Close()
		reference := primitive.Emit()
		defer reference.Close()

		So(actual.InstallProgram(compiled.Words), ShouldBeTrue)

		actualFrame := (*[primitive.WordCount]uint64)(unsafe.Pointer(actual))
		referenceFrame := (*[primitive.WordCount]uint64)(unsafe.Pointer(reference))

		for lane := 0; lane < primitive.ContextWords; lane++ {
			value := float64(lane + 1)
			gradient := float64(lane+1) / 8.0
			(*[primitive.ContextWords]float64)(unsafe.Pointer(&actualFrame[primitive.ContextStartWord]))[lane] = value
			(*[primitive.GradientWords]float64)(unsafe.Pointer(&actualFrame[primitive.GradientStartWord]))[lane] = gradient
			(*[primitive.ContextWords]float64)(unsafe.Pointer(&referenceFrame[primitive.ContextStartWord]))[lane] = value
			(*[primitive.GradientWords]float64)(unsafe.Pointer(&referenceFrame[primitive.GradientStartWord]))[lane] = gradient
		}

		So(geometricFrameGeneric(unsafe.Pointer(reference), 0x10), ShouldBeTrue)
		HypercubeGossip(nil, []*primitive.Value{actual})

		for lane := 0; lane < primitive.SignalsWords; lane++ {
			So(actualFrame[primitive.SignalsStartWord+lane], ShouldEqual, referenceFrame[primitive.SignalsStartWord+lane])
		}
	})

	Convey("Given a geometric instruction with encoded operands and destination", t, func() {
		layout := program.Layout{
			Regions: map[string]program.RegionExtent{
				"tokens":   {Start: primitive.TokensStartWord, Words: primitive.TokensWords},
				"gradient": {Start: primitive.GradientStartWord, Words: primitive.GradientWords},
				"asset":    {Start: primitive.AssetStartWord, Words: primitive.AssetWords},
			},
		}
		compiled, err := program.Compile(`[ (asset[8,8] self) <= (tokens[0,8] compose gradient) <= community ]`, layout)
		So(err, ShouldBeNil)

		actual := primitive.Emit()
		defer actual.Close()

		reference := primitive.Emit()
		defer reference.Close()

		So(actual.InstallProgram(compiled.Words), ShouldBeTrue)

		actualFrame := (*[primitive.WordCount]uint64)(unsafe.Pointer(actual))
		referenceFrame := (*[primitive.WordCount]uint64)(unsafe.Pointer(reference))

		for lane := 0; lane < primitive.ContextWords; lane++ {
			left := float64(lane+2) / 3.0
			right := float64(lane+5) / 7.0
			(*[primitive.ContextWords]float64)(unsafe.Pointer(&actualFrame[primitive.TokensStartWord]))[lane] = left
			(*[primitive.GradientWords]float64)(unsafe.Pointer(&actualFrame[primitive.GradientStartWord]))[lane] = right
			(*[primitive.ContextWords]float64)(unsafe.Pointer(&referenceFrame[primitive.ContextStartWord]))[lane] = left
			(*[primitive.GradientWords]float64)(unsafe.Pointer(&referenceFrame[primitive.GradientStartWord]))[lane] = right
		}

		So(GeometricFrame(unsafe.Pointer(reference), 0x10), ShouldBeTrue)
		HypercubeGossip(nil, []*primitive.Value{actual})

		for lane := 0; lane < primitive.SignalsWords; lane++ {
			So(actualFrame[primitive.AssetStartWord+8+lane], ShouldEqual, referenceFrame[primitive.SignalsStartWord+lane])
			So(actualFrame[primitive.SignalsStartWord+lane], ShouldEqual, 0)
		}
	})
}

func BenchmarkPopcntWords(b *testing.B) {
	var words [64]uint64
	for idx := range words {
		words[idx] = uint64(idx+1) * 0x9E3779B97F4A7C15
	}

	b.Run("CSA", func(bb *testing.B) {
		for iteration := 0; iteration < bb.N; iteration++ {
			_ = popcntWords(words[:])
		}
	})
}
