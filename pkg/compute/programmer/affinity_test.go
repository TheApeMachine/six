package programmer

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
affinityRegionWords returns the number of 64-bit words spanned by the
affinity region. The region is 257 bits wide by default so the span
rounds up to 5 words (the fifth acting as the Fermat-tail saturation
witness).
*/
func affinityRegionWords() int {
	return int((core.Cfg.Value.Region.Affinity.Bits + 63) / 64)
}

/*
isZeroAffinity checks that every word in the affinity region is zero. Used
as the pre-execution precondition so the post-execution assertion cannot
trivially pass on a value that already had bits set.
*/
func isZeroAffinity(value *primitive.Value) bool {
	start := core.Cfg.Value.Region.Affinity.Start
	words := affinityRegionWords()

	for idx := range words {
		if (*value)[start+idx] != 0 {
			return false
		}
	}

	return true
}

/*
affinitySource is the same single-line program that cmd/cfg/config.yml
ships under programs.affinity: the XOR-accumulate fold of the tokens
region against itself across the substrate's 16-rotation sweep, written
into the five affinity words. Tests that don't load viper config inject
this source directly via NewProgram's inline path so the wiring under
test stays identical to production.
*/
const affinitySource = "tokens[0,16] tokens[0,16] affinity[0,5] xor accumulate"

/*
TestAffinityProgram_SetsAffinityRegion is the end-to-end verification the
user asked for: mint a real Value through NewValue so the tokens region
is populated with Morton-coded slot codes, confirm the affinity region is
untouched, run the affinity program through the CPU substrate, and
assert the affinity region is no longer zero.

This is the smallest possible test that proves the full chain — parser
→ compiler → stage → cpu.Backend.Execute (running the real
wordblock_arm64.s / wordblock_amd64.s substrate) → writeback — actually
updates the affinity region.
*/
func TestAffinityProgram_SetsAffinityRegion(t *testing.T) {
	Convey("Given a freshly minted Value and the affinity program source", t, func() {
		values, mintErr := primitive.NewValue([]byte("affinity witness payload"))

		So(mintErr, ShouldBeNil)
		So(len(values), ShouldBeGreaterThanOrEqualTo, 1)

		value := values[0]

		Convey("The affinity region should start zero", func() {
			// Precondition: NewValue must not touch affinity yet. This
			// guards the post-execution assertion from silently passing
			// on a value that already had bits set.
			So(isZeroAffinity(value), ShouldBeTrue)
		})

		Convey("When the affinity program runs through the CPU substrate", func() {
			program := NewProgram(affinitySource)
			tokens, cont, parseErr := NewParser(program).Parse()

			So(parseErr, ShouldBeNil)
			So(len(tokens), ShouldEqual, 1)

			compiler := NewCompiler(tokens, WithContinuation(cont))

			executable := NewExecutable(compiler, nil).WithInputs(
				[]*primitive.Value{value},
			)

			backend := cpu.NewBackend(context.Background())

			result, runErr := executable.Run(CPU, backend)

			Convey("Run should complete without error", func() {
				So(runErr, ShouldBeNil)
				So(result, ShouldNotBeNil)
			})

			Convey("The affinity region should contain at least one set bit", func() {
				// The substrate's 16-rotation LSH sweep writes a 64-byte
				// signature into the signals region; writeback XORs the
				// first five signal words into affinity[0..4]. For a
				// non-zero tokens region the resulting signature must
				// land at least one bit in the affinity span or the
				// wordblock kernel never ran.
				affStart := core.Cfg.Value.Region.Affinity.Start
				words := affinityRegionWords()

				nonZero := false

				for idx := 0; idx < words; idx++ {
					if (*result)[affStart+idx] != 0 {
						nonZero = true

						break
					}
				}

				So(nonZero, ShouldBeTrue)
			})

			Convey("The full tokens region should be preserved across the run", func() {
				// universalBitwiseV2 now reads srcA / srcB directly out
				// of the named region and only writes dst, so every
				// word of the tokens region must survive a pass whose
				// dst is affinity. Any mutation here would mean the
				// substrate is clobbering regions the program did not
				// name — the exact failure mode the new wire format
				// exists to eliminate.
				tokStart := core.Cfg.Value.Region.Tokens.Start
				tokWords := int(
					(core.Cfg.Value.Region.Tokens.Bits + 63) / 64,
				)

				for idx := 0; idx < tokWords; idx++ {
					So((*result)[tokStart+idx], ShouldEqual, (*value)[tokStart+idx])
				}
			})
		})

		Reset(func() {
			primitive.CloseAll(values)
		})
	})
}

func BenchmarkAffinityProgram_Run(b *testing.B) {
	values, mintErr := primitive.NewValue([]byte("affinity witness payload"))
	if mintErr != nil {
		b.Fatal(mintErr)
	}
	b.Cleanup(func() {
		primitive.CloseAll(values)
	})

	value := values[0]

	program := NewProgram(affinitySource)
	tokens, cont, parseErr := NewParser(program).Parse()
	if parseErr != nil {
		b.Fatal(parseErr)
	}

	compiler := NewCompiler(tokens, WithContinuation(cont))
	executable := NewExecutable(compiler, nil)
	backend := cpu.NewBackend(context.Background())

	b.ReportAllocs()
	b.ResetTimer()

	for iterationIndex := 0; iterationIndex < b.N; iterationIndex++ {
		input := *value
		executable.WithInputs([]*primitive.Value{&input})
		_, runErr := executable.Run(CPU, backend)
		if runErr != nil {
			b.Fatal(runErr)
		}
	}
}
