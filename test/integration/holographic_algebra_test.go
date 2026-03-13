package integration

import (
	"testing"

	"github.com/theapemachine/six/pkg/logic/semantic"
	"github.com/theapemachine/six/pkg/numeric"
	. "github.com/smartystreets/goconvey/convey"
)

func TestHolographicSemanticEngine(t *testing.T) {
	Convey("Given a unified Semantic Reasoning Engine powered by GF(257) Cancellation", t, func() {
		eng := semantic.NewEngine(
			semantic.EngineWithFact("The_Cat", "sat_on", "The_Mat"),
			semantic.EngineWithFact("Roy", "is_in", "Kitchen"),
			semantic.EngineWithFact("Sandra", "is_in", "Garden"),
		)
		calc := numeric.NewCalculus()

		Convey("When parsing Multi-Modal and Multi-Tonal Braid Superpositions", func() {
			braidCat := eng.Inject("Image_of_Cat", "is_a", "The_Cat")
			braidRoy := eng.Inject("Roy", "was_in", "Living_Room")

			Convey("It manages to extract correct spatial paths from temporal overrides", func() {
				// We test Semantic Time Shift ("Where WAS Roy?")
				loc, phase := eng.QueryObject(braidRoy, "Roy", "was_in")
				So(loc, ShouldEqual, "Living_Room")
				So(phase, ShouldEqual, calc.Sum("Living_Room"))

				// We test regular state ("Where IS Roy?")
				// Since we injected the fact previously (it was facts[1])
				loc2, _ := eng.QueryObject(eng.Inject("Roy", "is_in", "Kitchen"), "Roy", "is_in")
				So(loc2, ShouldEqual, "Kitchen")
			})

			Convey("Cross-Modal queries should resolve via shared Label Phases", func() {
				// Query "What is the Image_of_Cat?" -> expecting "The_Cat"
				tgt, _ := eng.QueryObject(braidCat, "Image_of_Cat", "is_a")
				So(tgt, ShouldEqual, "The_Cat")
			})

			Convey("It rejects Noise constraints cleanly (Destructive Interference)", func() {
				path := calc.Sum("Kitchen")
				antiPath := calc.Subtract(numeric.Phase(numeric.FermatPrime), path)
				res := calc.Add(path, antiPath)
				So(res, ShouldEqual, 0)
			})
		})
	})
}
