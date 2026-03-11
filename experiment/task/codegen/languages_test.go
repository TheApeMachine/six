package codegen

import (
	"strings"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/provider/local"
)

func TestLanguagesExperimentPrompts(t *testing.T) {
	gc.Convey("Given a languages experiment prompt set", t, func() {
		experiment := &LanguagesExperiment{
			mds: &multiDataset{
				datasets: []provider.Dataset{
					local.New([][]byte{
						[]byte(strings.Repeat("a", 80)),
						[]byte(strings.Repeat("b", 120)),
					}),
				},
				langNames: []string{"Local"},
			},
		}

		prompt := experiment.Prompts()
		visible0, right0 := prompt.VisibleStrings(0)
		visible1, right1 := prompt.VisibleStrings(1)

		gc.Convey("It should hold out the final 50 bytes rather than half the sample", func() {
			gc.So(len(prompt.HeldOut(0)), gc.ShouldEqual, 50)
			gc.So(len(prompt.HeldOut(1)), gc.ShouldEqual, 50)
			gc.So(len(visible0), gc.ShouldEqual, 30)
			gc.So(len(visible1), gc.ShouldEqual, 70)
			gc.So(right0, gc.ShouldEqual, "")
			gc.So(right1, gc.ShouldEqual, "")
			gc.So(prompt.Full(0), gc.ShouldEqual, strings.Repeat("a", 80))
			gc.So(prompt.Full(1), gc.ShouldEqual, strings.Repeat("b", 120))
		})
	})
}

func BenchmarkLanguagesExperimentPrompts(b *testing.B) {
	corpus := make([][]byte, 64)
	for idx := range corpus {
		corpus[idx] = []byte(strings.Repeat("func sample() { return value; }\n", 8))
	}

	experiment := &LanguagesExperiment{
		mds: &multiDataset{
			datasets:  []provider.Dataset{local.New(corpus)},
			langNames: []string{"Local"},
		},
	}

	b.ReportAllocs()

	for range b.N {
		prompt := experiment.Prompts()
		if len(prompt.HeldOut(0)) != 50 {
			b.Fatalf("unexpected holdout width: %d", len(prompt.HeldOut(0)))
		}
	}
}
