//go:build darwin && cgo

package metal

import (
	"testing"

	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/compute/program"
	"github.com/theapemachine/six/pkg/primitive"

	. "github.com/smartystreets/goconvey/convey"
)

func TestHypercubeGossip(t *testing.T) {
	if Available() == 0 {
		t.Skip("metal unavailable")
	}

	Convey("Given a resident AST program", t, func() {
		layout := program.Layout{
			Regions: map[string]program.RegionExtent{
				"tokens":  {Start: primitive.TokensStartWord, Words: primitive.TokensWords},
				"signals": {Start: primitive.SignalsStartWord, Words: primitive.SignalsWords},
			},
		}
		compiled, err := program.Compile(`[ { A(signals[0,1]) A(tokens[0,1]) A(tokens[1,1]) ^ } ]`, layout)
		So(err, ShouldBeNil)

		actual := primitive.Emit()
		defer actual.Close()
		reference := primitive.Emit()
		defer reference.Close()

		So(actual.InstallProgram(compiled.Words), ShouldBeTrue)
		So(reference.InstallProgram(compiled.Words), ShouldBeTrue)

		actual.Set(primitive.TokensStartWord, 0b1010)
		actual.Set(primitive.TokensStartWord+1, 0b1100)
		reference.Set(primitive.TokensStartWord, 0b1010)
		reference.Set(primitive.TokensStartWord+1, 0b1100)

		backend := NewBackend(0)
		defer backend.Close()

		spawned, err := backend.HypercubeGossip(nil, []*primitive.Value{actual})
		defer primitive.CloseAll(spawned)

		So(err, ShouldBeNil)
		So(spawned, ShouldBeEmpty)

		cpu.HypercubeGossip(nil, []*primitive.Value{reference})
		So(actual.Get(primitive.SignalsRegion)[0], ShouldEqual, reference.Get(primitive.SignalsRegion)[0])
	})
}

func BenchmarkHypercubeGossip(b *testing.B) {
	if Available() == 0 {
		b.Skip("metal unavailable")
	}

	layout := program.Layout{
		Regions: map[string]program.RegionExtent{
			"tokens":  {Start: primitive.TokensStartWord, Words: primitive.TokensWords},
			"signals": {Start: primitive.SignalsStartWord, Words: primitive.SignalsWords},
		},
	}
	compiled, err := program.Compile(`[ { A(signals[0,1]) A(tokens[0,1]) A(tokens[1,1]) ^ } ]`, layout)
	if err != nil {
		b.Fatal(err)
	}

	values := make([]*primitive.Value, 64)
	for idx := range values {
		values[idx] = primitive.Emit()
		if !values[idx].InstallProgram(compiled.Words) {
			b.Fatal("install program failed")
		}
		values[idx].Set(primitive.TokensStartWord, uint64(idx))
		values[idx].Set(primitive.TokensStartWord+1, uint64(idx<<1))
	}
	defer primitive.CloseAll(values)

	backend := NewBackend(0)
	defer backend.Close()

	b.ResetTimer()
	for idx := 0; idx < b.N; idx++ {
		if _, err := backend.HypercubeGossip(nil, values); err != nil {
			b.Fatal(err)
		}
	}
}
