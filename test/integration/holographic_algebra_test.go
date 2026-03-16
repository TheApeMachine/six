package integration

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/store/data/provider/local"
	"github.com/theapemachine/six/test"
)

func TestHolographicAlgebra_Integration(t *testing.T) {
	corpus := []string{
		"Roy was in the living room.",
		"Roy is in the kitchen.",
		"Roy will be in the garage.",
		"Image of cat is a cat.",
		"Image of dog is a dog.",
		"The quick brown fox jumps over the lazy dog.",
		"The quick red fox jumps over the sleeping dog.",
		"The quick silver fox runs past the awake dog.",
		"Force equals mass times acceleration.",
		"Gravity is an accelerating force.",
		"If it rains and you have no umbrella you get wet.",
		"If you have an umbrella or stay inside you stay dry.",
	}

	Convey("Given a holographic machine with ingested corpus", t, func() {
		helper := test.NewTestHelper()
		defer helper.Teardown()

		So(helper.SetDataset(local.New(local.WithStrings(corpus))), ShouldBeNil)

		Convey("Temporal: 'was' routes to living room not kitchen", func() {
			result, err := helper.Prompt("Roy was in the ")
			So(err, ShouldBeNil)
			So(string(result), ShouldEqual, "living room")
			So(string(result), ShouldNotContainSubstring, "kitchen")
		})

		Convey("Temporal: 'is' routes to kitchen not living room", func() {
			result, err := helper.Prompt("Roy is in the ")
			So(err, ShouldBeNil)
			So(string(result), ShouldEqual, "kitchen")
			So(string(result), ShouldNotContainSubstring, "living room")
		})

		Convey("Temporal: 'will be' routes to garage", func() {
			result, err := helper.Prompt("Roy will be in the ")
			So(err, ShouldBeNil)
			So(string(result), ShouldEqual, "garage")
		})

		Convey("Cross-modal: cat prompt returns cat not dog-unique content", func() {
			result, err := helper.Prompt("Image of cat ")
			So(err, ShouldBeNil)
			So(string(result), ShouldEqual, "cat")
		})

		Convey("Cross-modal: dog prompt returns dog not cat-unique content", func() {
			result, err := helper.Prompt("Image of dog ")
			So(err, ShouldBeNil)
			So(string(result), ShouldEqual, "dog")
		})

		Convey("Superposition: shared prefix activates at least one branch", func() {
			result, err := helper.Prompt("The quick ")
			So(err, ShouldBeNil)
			So(string(result), ShouldNotBeEmpty)
		})

		Convey("Determinism: same prompt returns same result twice", func() {
			first, err := helper.Prompt("Roy is in the ")
			So(err, ShouldBeNil)

			second, err := helper.Prompt("Roy is in the ")
			So(err, ShouldBeNil)

			So(string(second), ShouldEqual, string(first))
		})

		Convey("Logic AND: rains-and context routes to wet not dry", func() {
			result, err := helper.Prompt("If it rains and ")
			So(err, ShouldBeNil)
			So(string(result), ShouldEqual, "wet")
		})

		Convey("Logic OR: umbrella-or context routes to dry not wet", func() {
			result, err := helper.Prompt("If you have an umbrella or ")
			So(err, ShouldBeNil)
			So(string(result), ShouldEqual, "dry")
		})
	})
}

func BenchmarkHolographicAlgebra(b *testing.B) {
	corpus := []string{
		"Roy was in the living room.",
		"Roy is in the kitchen.",
		"Roy will be in the garage.",
		"The quick brown fox jumps over the lazy dog.",
		"The quick red fox jumps over the sleeping dog.",
		"Force equals mass times acceleration.",
	}

	helper := test.NewTestHelper()
	defer helper.Teardown()

	if err := helper.SetDataset(local.New(local.WithStrings(corpus))); err != nil {
		b.Fatal(err)
	}

	queries := []string{
		"Roy is in the ",
		"The quick brown ",
		"Force equals mass ",
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		result, err := helper.Prompt(queries[i%len(queries)])
		if err != nil {
			b.Fatal(err)
		}

		if len(result) == 0 {
			b.Fatal("result should not be empty")
		}
	}
}
