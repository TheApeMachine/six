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

		So(helper.Machine.SetDataset(
			local.New(local.WithStrings(corpus)),
		), ShouldBeNil)

		Convey("It should return the exact living-room suffix for 'was'", func() {
			result, err := helper.Machine.Prompt("Roy was in the ")

			So(err, ShouldBeNil)
			So(string(result), ShouldEqual, "living room.")
			So(string(result), ShouldNotContainSubstring, "kitchen")
		})

		Convey("It should return the exact kitchen suffix for 'is'", func() {
			result, err := helper.Machine.Prompt("Roy is in the ")

			So(err, ShouldBeNil)
			So(string(result), ShouldEqual, "kitchen.")
			So(string(result), ShouldNotContainSubstring, "living room")
		})

		Convey("It should return the exact garage suffix for 'will be'", func() {
			result, err := helper.Machine.Prompt("Roy will be in the ")

			So(err, ShouldBeNil)
			So(string(result), ShouldEqual, "garage.")
		})

		Convey("It should return the exact cat suffix for cat prompt", func() {
			result, err := helper.Machine.Prompt("Image of cat ")

			So(err, ShouldBeNil)
			So(string(result), ShouldEqual, "is a cat.")
		})

		Convey("It should return the exact dog suffix for dog prompt", func() {
			result, err := helper.Machine.Prompt("Image of dog ")

			So(err, ShouldBeNil)
			So(string(result), ShouldEqual, "is a dog.")
		})

		Convey("It should resolve shared prefix to the longest exact branch", func() {
			result, err := helper.Machine.Prompt("The quick ")

			So(err, ShouldBeNil)
			So(string(result), ShouldEqual, "red fox jumps over the sleeping dog.")
		})

		Convey("It should return same result twice for same prompt", func() {
			first, err := helper.Machine.Prompt("Roy is in the ")
			So(err, ShouldBeNil)

			second, err := helper.Machine.Prompt("Roy is in the ")
			So(err, ShouldBeNil)

			So(string(second), ShouldEqual, string(first))
		})

		Convey("It should return the exact wet suffix for rains-and context", func() {
			result, err := helper.Machine.Prompt("If it rains and ")

			So(err, ShouldBeNil)
			So(string(result), ShouldEqual, "you have no umbrella you get wet.")
		})

		Convey("It should return the exact dry suffix for umbrella-or context", func() {
			result, err := helper.Machine.Prompt("If you have an umbrella or ")

			So(err, ShouldBeNil)
			So(string(result), ShouldEqual, "stay inside you stay dry.")
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

	if err := helper.Machine.SetDataset(
		local.New(local.WithStrings(corpus)),
	); err != nil {
		b.Fatal(err)
	}

	queries := []string{
		"Roy is in the ",
		"The quick brown ",
		"Force equals mass ",
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = helper.Machine.Prompt(
			queries[i%len(queries)],
		)
	}
}
