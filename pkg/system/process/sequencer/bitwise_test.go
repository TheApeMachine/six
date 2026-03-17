package sequencer

import (
	"fmt"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/store/data"
)

/*
bitwiseSequence converts string chunks into a sequence of value fragments.
*/
func bitwiseSequence(chunks []string) [][]data.Value {
	sequence := make([][]data.Value, len(chunks))

	for i, chunk := range chunks {
		fragment := make([]data.Value, 0, len(chunk))

		for _, symbol := range []byte(chunk) {
			fragment = append(fragment, data.BaseValue(byte(symbol)))
		}

		sequence[i] = fragment
	}

	return sequence
}

/*
assertBitwiseSequence verifies that a sequence of fragments matches expected strings.
*/
func assertBitwiseSequence(expected []string, actual [][]data.Value) {
	gc.So(len(actual), gc.ShouldEqual, len(expected))

	for chunkIndex, chunk := range expected {
		gc.So(bitwiseFragmentString(actual[chunkIndex]), gc.ShouldEqual, chunk)
	}
}

/*
bitwiseFragmentString decodes a value fragment back into its original string.
*/
func bitwiseFragmentString(fragment []data.Value) string {
	decoded := make([]byte, len(fragment))

	for i, value := range fragment {
		found := false

		for symbol := 0; symbol <= 255; symbol++ {
			expected := data.BaseValue(byte(symbol))
			shared := value.AND(expected)

			if shared.ActiveCount() == value.ActiveCount() &&
				shared.ActiveCount() == expected.ActiveCount() {
				decoded[i] = byte(symbol)
				found = true
				break
			}
		}

		gc.So(found, gc.ShouldBeTrue)
	}

	return string(decoded)
}

func TestBitwiseHealerWrite(t *testing.T) {
	gc.Convey("Given a new BitwiseHealer", t, func() {
		bitwise := NewBitwiseHealer()

		gc.So(bitwise, gc.ShouldNotBeNil)
		gc.So(bitwise.buffer, gc.ShouldBeEmpty)

		gc.Convey("When writing one fragmented sequence", func() {
			bitwise.Write(bitwiseSequence([]string{"Roy was ", "in t"}))

			gc.So(bitwise.Len(), gc.ShouldEqual, 1)
			assertBitwiseSequence([]string{"Roy was ", "in t"}, bitwise.EntryAt(0))
		})

		gc.Convey("When writing beyond buffer capacity", func() {
			for i := 0; i < 1025; i++ {
				bitwise.Write(bitwiseSequence([]string{fmt.Sprintf("%05d", i)}))
			}

			gc.So(bitwise.Len(), gc.ShouldEqual, 1024)
			assertBitwiseSequence([]string{"00001"}, bitwise.EntryAt(0))
			assertBitwiseSequence([]string{"01024"}, bitwise.EntryAt(1023))
		})
	})
}

func TestBitwiseHealerHeal(t *testing.T) {
	royBroken := [][]string{
		{"Roy was ", "in t", "he liv", "ing ", "ro", "om"},
		{"Roy is ", "i", "n the k", "itche", "n"},
		{"Roy will be ", "in", " t", "he", " g", "ara", "ge"},
	}

	royHealed := [][]string{
		{"Roy was", " in the ", "living room"},
		{"Roy is", " in the ", "kitchen"},
		{"Roy will be", " in the ", "garage"},
	}

	imageBroken := [][]string{
		{"Image of ", "ca", "t ", "is a ", "ca", "t"},
		{"Image of ", "do", "g ", "is a ", "do", "g"},
	}

	imageHealed := [][]string{
		{"Image of ", "cat", " is a ", "cat"},
		{"Image of ", "dog", " is a ", "dog"},
	}

	gc.Convey("Given a new BitwiseHealer", t, func() {
		bitwise := NewBitwiseHealer()

		gc.So(bitwise, gc.ShouldNotBeNil)

		gc.Convey("When only one fragmented sequence is buffered", func() {
			original := []string{"Roy was ", "in t", "he liv", "ing ", "ro", "om"}

			bitwise.Write(bitwiseSequence(original))

			healed := bitwise.Heal()

			gc.So(len(healed), gc.ShouldEqual, 1)
			assertBitwiseSequence(original, healed[0])
		})

		gc.Convey("When a Roy cluster shares a broken anchor", func() {
			for _, sequence := range royBroken {
				bitwise.Write(bitwiseSequence(sequence))
			}

			healed := bitwise.Heal()

			gc.So(len(healed), gc.ShouldEqual, len(royHealed))

			for i, expected := range royHealed {
				assertBitwiseSequence(expected, healed[i])
			}
		})

		gc.Convey("When different overlap families are buffered together", func() {
			for _, sequence := range royBroken {
				bitwise.Write(bitwiseSequence(sequence))
			}

			for _, sequence := range imageBroken {
				bitwise.Write(bitwiseSequence(sequence))
			}

			healed := bitwise.Heal()

			gc.So(len(healed), gc.ShouldEqual, len(royHealed)+len(imageHealed))

			for i, expected := range royHealed {
				assertBitwiseSequence(expected, healed[i])
			}

			for i, expected := range imageHealed {
				assertBitwiseSequence(expected, healed[len(royHealed)+i])
			}
		})

		gc.Convey("When sequences share no structural anchor", func() {
			original := [][]string{
				{"fj", "ord"},
				{"ny", "mph"},
			}

			for _, sequence := range original {
				bitwise.Write(bitwiseSequence(sequence))
			}

			healed := bitwise.Heal()

			gc.So(len(healed), gc.ShouldEqual, len(original))

			for i, expected := range original {
				assertBitwiseSequence(expected, healed[i])
			}
		})
	})
}

func BenchmarkBitwiseHealerHeal(b *testing.B) {
	sequences := [][][]data.Value{
		bitwiseSequence([]string{"Roy was ", "in t", "he liv", "ing ", "ro", "om"}),
		bitwiseSequence([]string{"Roy is ", "i", "n the k", "itche", "n"}),
		bitwiseSequence([]string{"Roy will be ", "in", " t", "he", " g", "ara", "ge"}),
		bitwiseSequence([]string{"Image of ", "ca", "t ", "is a ", "ca", "t"}),
		bitwiseSequence([]string{"Image of ", "do", "g ", "is a ", "do", "g"}),
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		bitwise := NewBitwiseHealer()

		for _, sequence := range sequences {
			bitwise.Write(sequence)
		}

		_ = bitwise.Heal()
	}
}
