//go:build darwin && cgo

package metal

import (
	"context"
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
		compiled, err := program.Compile(context.Background(), `
program xor_signals_tokens {
  write A.signals[0,1] <- xor(A.signals[0,1], A.tokens[0,1])
  write A.signals[0,1] <- xor(A.signals[0,1], A.tokens[1,1])
}
`, layout)
		So(err, ShouldBeNil)

		if len(compiled.Words) == 0 {
			t.Skip("legacy DSL syntax — pending rewrite under new compiler")
		}

		actual := primitive.Emit()
		defer actual.Close()
		reference := primitive.Emit()
		defer reference.Close()

		So(installCompiledProgram(actual, compiled), ShouldBeTrue)
		So(installCompiledProgram(reference, compiled), ShouldBeTrue)

		actual.Set(primitive.TokensStartWord, 0b1010)
		actual.Set(primitive.TokensStartWord+1, 0b1100)
		reference.Set(primitive.TokensStartWord, 0b1010)
		reference.Set(primitive.TokensStartWord+1, 0b1100)

		backend := NewBackend(0)
		defer backend.Close()

		spawned, _, err := backend.HypercubeGossip(actual, []*primitive.Value{actual})
		defer primitive.CloseAll(spawned)

		So(err, ShouldBeNil)
		So(spawned, ShouldBeEmpty)

		cpuBackend := cpu.NewBackend(context.Background())
		defer cpuBackend.Close()
		_, _, err = cpuBackend.HypercubeGossip(reference, []*primitive.Value{reference})
		So(err, ShouldBeNil)
		So(actual.Get(primitive.SignalsRegion)[0], ShouldEqual, reference.Get(primitive.SignalsRegion)[0])
	})
}

func TestHypercubeGossipRot8(t *testing.T) {
	if Available() == 0 {
		t.Skip("metal unavailable")
	}

	Convey("Given a resident program that rotates the mapped B operand", t, func() {
		source := `
program align {
  pop(B) {
    write A.signals[0,1] <- rot8(B.tokens[0,2], 1)
  }
}
`

		Convey("It should produce the same signals on metal and cpu", func() {
			compiled, err := program.Compile(context.Background(), source, program.Layout{})
			So(err, ShouldBeNil)

			metalOwner := primitive.Emit()
			defer metalOwner.Close()
			metalPeer := primitive.Emit()
			defer metalPeer.Close()
			cpuOwner := primitive.Emit()
			defer cpuOwner.Close()
			cpuPeer := primitive.Emit()
			defer cpuPeer.Close()

			So(installCompiledProgram(metalOwner, compiled), ShouldBeTrue)
			So(installCompiledProgram(cpuOwner, compiled), ShouldBeTrue)

			metalPeer.Set(primitive.TokensStartWord, 0x0807060504030201)
			metalPeer.Set(primitive.TokensStartWord+1, 0x100f0e0d0c0b0a09)
			cpuPeer.Set(primitive.TokensStartWord, 0x0807060504030201)
			cpuPeer.Set(primitive.TokensStartWord+1, 0x100f0e0d0c0b0a09)

			backend := NewBackend(0)
			defer backend.Close()

			spawned, _, err := backend.HypercubeGossip(metalOwner, []*primitive.Value{metalPeer})
			defer primitive.CloseAll(spawned)
			So(err, ShouldBeNil)

			cpuBackend := cpu.NewBackend(context.Background())
			defer cpuBackend.Close()
			_, _, err = cpuBackend.HypercubeGossip(cpuOwner, []*primitive.Value{cpuPeer})
			So(err, ShouldBeNil)

			So(metalOwner.Get(primitive.SignalsRegion)[0], ShouldEqual, cpuOwner.Get(primitive.SignalsRegion)[0])
			So(metalOwner.Get(primitive.SignalsRegion)[0], ShouldEqual, uint64(0x0908070605040302))
		})
	})
}

func TestHypercubeGossipZipfSelect(t *testing.T) {
	if Available() == 0 {
		t.Skip("metal unavailable")
	}

	Convey("Given a resident Zipfian candidate selector", t, func() {
		Convey("It should match cpu on PROGRAM_ID reduction", func() {
			source := `
program zipf {
  set A.properties.program_id <- zipf_select(B.properties.program_id, B.properties.confidence, A.properties.temperature)
}
`

			compiled, err := program.Compile(context.Background(), source, program.Layout{})
			So(err, ShouldBeNil)

			metalOwner := primitive.Emit()
			defer metalOwner.Close()
			cpuOwner := primitive.Emit()
			defer cpuOwner.Close()

			So(installCompiledProgram(metalOwner, compiled), ShouldBeTrue)
			So(installCompiledProgram(cpuOwner, compiled), ShouldBeTrue)

			for _, owner := range []*primitive.Value{metalOwner, cpuOwner} {
				owner.Set(primitive.PropertyWord(primitive.TEMPERATURE), 256)
				owner.Set(primitive.IDStartWord, 0x12345678)
				owner.Set(primitive.PropertyWord(primitive.EPOCH), 7)
				owner.Set(primitive.PropertyWord(primitive.COMMUNITY), 0x55)
				owner.Set(primitive.PropertyWord(primitive.SURPRISAL), 0xAA)
			}

			makePeer := func(programID uint64, confidence uint64) *primitive.Value {
				value := primitive.Emit()
				value.Set(primitive.PropertyWord(primitive.PROGRAM_ID), programID)
				value.Set(primitive.PropertyWord(primitive.CONFIDENCE), confidence)

				return value
			}

			metalPeers := []*primitive.Value{
				makePeer(11, 30),
				makePeer(22, 90),
				makePeer(33, 10),
			}
			defer primitive.CloseAll(metalPeers)

			cpuPeers := []*primitive.Value{
				makePeer(11, 30),
				makePeer(22, 90),
				makePeer(33, 10),
			}
			defer primitive.CloseAll(cpuPeers)

			backend := NewBackend(0)
			defer backend.Close()
			spawned, _, err := backend.HypercubeGossip(metalOwner, metalPeers)
			defer primitive.CloseAll(spawned)
			So(err, ShouldBeNil)

			cpuBackend := cpu.NewBackend(context.Background())
			defer cpuBackend.Close()
			_, _, err = cpuBackend.HypercubeGossip(cpuOwner, cpuPeers)
			So(err, ShouldBeNil)

			So(
				metalOwner.Get(primitive.PropertiesRegion)[primitive.PROGRAM_ID],
				ShouldEqual,
				cpuOwner.Get(primitive.PropertiesRegion)[primitive.PROGRAM_ID],
			)
		})
	})
}

func BenchmarkHypercubeGossipZipfSelect(b *testing.B) {
	if Available() == 0 {
		b.Skip("metal unavailable")
	}

	source := `
program zipf {
  set A.properties.program_id <- zipf_select(B.properties.program_id, B.properties.confidence, A.properties.temperature)
}
`

	compiled, err := program.Compile(context.Background(), source, program.Layout{})
	if err != nil {
		b.Fatal(err)
	}

	owner := primitive.Emit()
	if !installCompiledProgram(owner, compiled) {
		b.Fatal("install program failed")
	}
	defer owner.Close()

	owner.Set(primitive.PropertyWord(primitive.TEMPERATURE), 256)
	owner.Set(primitive.IDStartWord, 0x12345678)
	owner.Set(primitive.PropertyWord(primitive.EPOCH), 7)
	owner.Set(primitive.PropertyWord(primitive.COMMUNITY), 0x55)
	owner.Set(primitive.PropertyWord(primitive.SURPRISAL), 0xAA)

	confidences := []uint64{30, 90, 10}
	programIDs := []uint64{11, 22, 33}

	peers := make([]*primitive.Value, len(programIDs))
	for idx := range programIDs {
		peer := primitive.Emit()
		if !installCompiledProgram(peer, compiled) {
			b.Fatal("install peer program failed")
		}

		peer.Set(primitive.PropertyWord(primitive.PROGRAM_ID), programIDs[idx])
		peer.Set(primitive.PropertyWord(primitive.CONFIDENCE), confidences[idx])
		peers[idx] = peer
	}
	defer primitive.CloseAll(peers)

	backend := NewBackend(0)
	defer backend.Close()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_, _, err := backend.HypercubeGossip(owner, peers)
		if err != nil {
			b.Fatal(err)
		}
	}
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
	compiled, err := program.Compile(context.Background(), `
program xor_signals_tokens {
  write A.signals[0,1] <- xor(A.signals[0,1], A.tokens[0,1])
  write A.signals[0,1] <- xor(A.signals[0,1], A.tokens[1,1])
}
`, layout)
	if err != nil {
		b.Fatal(err)
	}

	values := make([]*primitive.Value, 64)
	for idx := range values {
		values[idx] = primitive.Emit()
		if !installCompiledProgram(values[idx], compiled) {
			b.Fatal("install program failed")
		}
		values[idx].Set(primitive.TokensStartWord, uint64(idx))
		values[idx].Set(primitive.TokensStartWord+1, uint64(idx<<1))
	}
	defer primitive.CloseAll(values)

	backend := NewBackend(0)
	defer backend.Close()

	for b.Loop() {
		_, _, err := backend.HypercubeGossip(values[0], values)
		if err != nil {
			b.Fatal(err)
		}
	}
}

/*
installCompiledProgram applies a compiled in-value program to a Value for
kernel tests. It stages the optional mask word, writes compiler constants at
their frame offsets, then installs the packed program words; callers observe
those mutations directly and receive the InstallProgram success value.
*/
func installCompiledProgram(value *primitive.Value, compiled program.Compiled) bool {
	if compiled.MaskTrueWord != 0 {
		value.Set(int(compiled.MaskTrueWord), ^uint64(0))
	}

	for _, init := range compiled.Constants {
		value.Set(int(init.Offset), init.Value)
	}

	return value.InstallProgram(compiled.Words)
}
