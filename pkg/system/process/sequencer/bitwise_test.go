package sequencer

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

var cases = []struct {
	name   string
	broken [][]string
	healed [][]string
}{
	{
		name: "Roy cluster shares a broken anchor",
		broken: [][]string{
			{"Roy was ", "in t", "he liv", "ing ", "ro", "om"},
			{"Roy is ", "i", "n the k", "itche", "n"},
			{"Roy will be ", "in", " t", "he", " g", "ara", "ge"},
		},
		healed: [][]string{
			{"Roy was", " in the ", "living room"},
			{"Roy is", " in the ", "kitchen"},
			{"Roy will be", " in the ", "garage"},
		},
	},
	{
		name: "Image cluster shares a broken anchor",
		broken: [][]string{
			{"Image of ", "ca", "t ", "is a ", "ca", "t"},
			{"Image of ", "do", "g ", "is a ", "do", "g"},
		},
		healed: [][]string{
			{"Image of ", "cat", " is a ", "cat"},
			{"Image of ", "dog", " is a ", "dog"},
		},
	},
}

func TestBitwiseHealerHeal(t *testing.T) {
	for _, tc := range cases {
		gc.Convey("Given "+tc.name, t, func() {
			bitwise := NewBitwiseHealer()

			for _, sequence := range tc.broken {
				for _, fragment := range sequence {
					for idx, b := range []byte(fragment) {
						bitwise.Write(b, idx == len(fragment)-1)
					}
				}
			}

			healed := bitwise.Heal()

			var expected []string

			for _, seq := range tc.healed {
				expected = append(expected, seq...)
			}

			for idx, chunk := range healed {
				t.Logf("healed[%d] = %q", idx, string(chunk))
			}

			gc.So(len(healed), gc.ShouldEqual, len(expected))

			for idx, chunk := range healed {
				gc.So(string(chunk), gc.ShouldEqual, expected[idx])
			}
		})
	}
}

func BenchmarkBitwiseHealerHeal(b *testing.B) {
	for b.Loop() {
		bitwise := NewBitwiseHealer()

		for _, sequence := range cases[0].broken {
			for _, fragment := range sequence {
				for idx, bv := range []byte(fragment) {
					bitwise.Write(bv, idx == len(fragment)-1)
				}
			}
		}

		_ = bitwise.Heal()
	}
}
