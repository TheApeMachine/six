package semantic

import (
	"fmt"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
)

func TestNegativeConstraints(t *testing.T) {
	gc.Convey("Given an engine with positive and negative facts", t, func() {
		eng := NewEngine()

		posPhase := eng.Inject("Cat", "is_on", "Mat")
		negPhase := eng.InjectNegation("Cat", "is_on", "Mat")

		gc.Convey("The anti-braid should be the additive inverse", func() {
			sum := eng.calc.Add(posPhase, negPhase)
			gc.So(sum, gc.ShouldEqual, numeric.Phase(0))
		})

		gc.Convey("Merging positive + negative should yield exactly zero", func() {
			merged := eng.Merge([]numeric.Phase{posPhase, negPhase})
			gc.So(merged, gc.ShouldEqual, numeric.Phase(0))
		})

		gc.Convey("Interference should detect cancellation", func() {
			gc.So(eng.Interference(posPhase, negPhase), gc.ShouldBeTrue)
		})

		gc.Convey("Interference should reject non-cancelling pairs", func() {
			other := eng.Inject("Dog", "is_in", "Yard")
			gc.So(eng.Interference(posPhase, other), gc.ShouldBeFalse)
		})

		gc.Convey("Negation should not corrupt existing positive queries", func() {
			eng2 := NewEngine()
			eng2.Inject("Cat", "is_on", "Mat")
			eng2.Inject("Dog", "is_in", "Yard")
			eng2.InjectNegation("Cat", "is_on", "Mat")

			braid := eng2.calc.Multiply(
				eng2.calc.Multiply(eng2.calc.Sum("Dog"), eng2.calc.Sum("is_in")),
				eng2.calc.Sum("Yard"),
			)
			name, _, err := eng2.QueryObject(braid, "Dog", "is_in")
			gc.So(err, gc.ShouldBeNil)
			gc.So(name, gc.ShouldEqual, "Yard")
		})
	})
}

func TestNegativeConstraintsAtScale(t *testing.T) {
	gc.Convey("Given 50 facts with 10 negations", t, func() {
		eng := NewEngine()

		var phases []numeric.Phase

		for i := 0; i < 50; i++ {
			p := eng.Inject(
				fmt.Sprintf("Person%d", i),
				"is_in",
				fmt.Sprintf("Room%d", i%10),
			)
			phases = append(phases, p)
		}

		var negPhases []numeric.Phase

		for i := 0; i < 10; i++ {
			np := eng.InjectNegation(
				fmt.Sprintf("Person%d", i),
				"is_in",
				fmt.Sprintf("Room%d", i%10),
			)
			negPhases = append(negPhases, np)
		}

		gc.Convey("Each negation should exactly cancel its positive", func() {
			for i := 0; i < 10; i++ {
				sum := eng.calc.Add(phases[i], negPhases[i])
				gc.So(sum, gc.ShouldEqual, numeric.Phase(0))
			}
		})

		gc.Convey("Un-negated facts should still be queryable", func() {
			for i := 10; i < 50; i++ {
				name, _, err := eng.QueryObject(phases[i], fmt.Sprintf("Person%d", i), "is_in")
				gc.So(err, gc.ShouldBeNil)
				gc.So(name, gc.ShouldEqual, fmt.Sprintf("Room%d", i%10))
			}
		})
	})
}

func TestTemporalLogic(t *testing.T) {
	gc.Convey("Given temporal facts across three time steps", t, func() {
		eng := NewEngine()

		past := eng.InjectTemporal("Sandra", "is_in", "Garden", 1)
		present := eng.InjectTemporal("Sandra", "is_in", "Kitchen", 2)
		future := eng.InjectTemporal("Sandra", "is_in", "Office", 3)

		gc.Convey("All three braids should be distinct", func() {
			gc.So(past, gc.ShouldNotEqual, present)
			gc.So(present, gc.ShouldNotEqual, future)
			gc.So(past, gc.ShouldNotEqual, future)
		})

		gc.Convey("Temporal markers should be stored correctly", func() {
			gc.So(eng.facts[0].Temporal, gc.ShouldEqual, numeric.Phase(1))
			gc.So(eng.facts[1].Temporal, gc.ShouldEqual, numeric.Phase(2))
			gc.So(eng.facts[2].Temporal, gc.ShouldEqual, numeric.Phase(3))
		})

		gc.Convey("Same S-V with different temporal yields different phases", func() {
			sameLink := eng.InjectTemporal("Sandra", "is_in", "Garden", 2)
			gc.So(sameLink, gc.ShouldNotEqual, past)
		})

		gc.Convey("All temporal braids should be non-zero and within GF(257)", func() {
			for _, p := range []numeric.Phase{past, present, future} {
				gc.So(uint32(p), gc.ShouldBeGreaterThan, 0)
				gc.So(uint32(p), gc.ShouldBeLessThan, numeric.FermatPrime)
			}
		})
	})
}

func TestDeBraid(t *testing.T) {
	gc.Convey("Given three merged facts", t, func() {
		eng := NewEngine()

		phaseA := eng.Inject("Cat", "is_on", "Mat")
		phaseB := eng.Inject("Dog", "is_in", "Yard")
		phaseC := eng.Inject("Bird", "flew_over", "Wall")

		merged := eng.Merge([]numeric.Phase{phaseA, phaseB, phaseC})

		gc.Convey("DeBraid(merged, A) should equal Merge(B, C)", func() {
			residual := eng.DeBraid(merged, phaseA)
			expected := eng.Merge([]numeric.Phase{phaseB, phaseC})

			gc.So(residual, gc.ShouldEqual, expected)
		})

		gc.Convey("DeBraidFact by S-V-O should equal DeBraid by phase", func() {
			byPhase := eng.DeBraid(merged, phaseA)
			bySVO, err := eng.DeBraidFact(merged, "Cat", "is_on", "Mat")

			gc.So(err, gc.ShouldBeNil)
			gc.So(bySVO, gc.ShouldEqual, byPhase)
		})

		gc.Convey("Sequential DeBraid should isolate each fact", func() {
			r1 := eng.DeBraid(merged, phaseA)
			r2 := eng.DeBraid(r1, phaseB)

			gc.So(r2, gc.ShouldEqual, phaseC)

			r3 := eng.DeBraid(merged, phaseB)
			r4 := eng.DeBraid(r3, phaseC)

			gc.So(r4, gc.ShouldEqual, phaseA)
		})

		gc.Convey("DeBraid is commutative — order doesn't matter", func() {
			abc := eng.DeBraid(eng.DeBraid(merged, phaseA), phaseB)
			bac := eng.DeBraid(eng.DeBraid(merged, phaseB), phaseA)

			gc.So(abc, gc.ShouldEqual, bac)
			gc.So(abc, gc.ShouldEqual, phaseC)
		})

		gc.Convey("DeBraid of all three facts should yield zero", func() {
			r := eng.DeBraid(eng.DeBraid(eng.DeBraid(merged, phaseA), phaseB), phaseC)
			gc.So(r, gc.ShouldEqual, numeric.Phase(0))
		})
	})

	gc.Convey("Given 20 merged facts, DeBraid should isolate any single one", t, func() {
		eng := NewEngine()

		var phases []numeric.Phase

		for i := 0; i < 20; i++ {
			p := eng.Inject(
				fmt.Sprintf("Entity%d", i),
				fmt.Sprintf("action%d", i%5),
				fmt.Sprintf("Target%d", i%7),
			)
			phases = append(phases, p)
		}

		merged := eng.Merge(phases)

		for target := 0; target < 20; target++ {
			residual := merged

			for i := 0; i < 20; i++ {
				if i == target {
					continue
				}

				residual = eng.DeBraid(residual, phases[i])
			}

			gc.So(residual, gc.ShouldEqual, phases[target])
		}
	})
}

func TestQueryLink(t *testing.T) {
	gc.Convey("Given an engine with multiple facts", t, func() {
		eng := NewEngine()

		braids := make(map[string]numeric.Phase)

		facts := [][3]string{
			{"Cat", "sat_on", "Mat"},
			{"Dog", "ran_to", "Park"},
			{"Bird", "flew_over", "Lake"},
			{"Fish", "swam_in", "Ocean"},
			{"Fox", "jumped_over", "Fence"},
		}

		for _, fact := range facts {
			braids[fact[1]] = eng.Inject(fact[0], fact[1], fact[2])
		}

		gc.Convey("QueryLink should recover the correct verb for each fact", func() {
			for _, fact := range facts {
				link, _, err := eng.QueryLink(braids[fact[1]], fact[0], fact[2])
				gc.So(err, gc.ShouldBeNil)
				gc.So(link, gc.ShouldEqual, fact[1])
			}
		})
	})
}

func TestPhaseCollisions(t *testing.T) {
	gc.Convey("Given strings that hash to the same GF(257) phase", t, func() {
		eng := NewEngine()
		calc := numeric.NewCalculus()

		var colliders []string

		for i := 0; i < 10000; i++ {
			s := fmt.Sprintf("str_%d", i)

			if calc.Sum(s) == calc.Sum("Cat") && s != "Cat" {
				colliders = append(colliders, s)
			}
		}

		gc.Convey("Colliders should exist (GF(257) has only 256 non-zero phases)", func() {
			gc.So(len(colliders), gc.ShouldBeGreaterThan, 0)
		})

		gc.Convey("QueryObject with a colliding subject should still work via scan", func() {
			if len(colliders) == 0 {
				gc.So(true, gc.ShouldBeTrue)
				return
			}

			collider := colliders[0]
			eng.Inject("Cat", "is_on", "Mat")
			eng.Inject(collider, "likes", "Fish")

			catBraid := eng.calc.Multiply(
				eng.calc.Multiply(eng.calc.Sum("Cat"), eng.calc.Sum("is_on")),
				eng.calc.Sum("Mat"),
			)

			name, _, err := eng.QueryObject(catBraid, "Cat", "is_on")
			gc.So(err, gc.ShouldBeNil)
			gc.So(name, gc.ShouldEqual, "Mat")
		})
	})
}

func BenchmarkInjectNegation(b *testing.B) {
	eng := NewEngine()

	for i := 0; i < 100; i++ {
		eng.Inject(fmt.Sprintf("E%d", i), "rel", fmt.Sprintf("T%d", i))
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		eng.InjectNegation(fmt.Sprintf("E%d", i%100), "rel", fmt.Sprintf("T%d", i%100))
	}
}

func BenchmarkDeBraid(b *testing.B) {
	eng := NewEngine()

	var phases []numeric.Phase

	for i := 0; i < 50; i++ {
		p := eng.Inject(fmt.Sprintf("E%d", i), "rel", fmt.Sprintf("T%d", i))
		phases = append(phases, p)
	}

	merged := eng.Merge(phases)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		eng.DeBraid(merged, phases[i%50])
	}
}

func BenchmarkInjectTemporal(b *testing.B) {
	eng := NewEngine()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		eng.InjectTemporal(
			fmt.Sprintf("Person%d", i%100),
			"is_in",
			fmt.Sprintf("Room%d", i%10),
			numeric.Phase(i%256),
		)
	}
}

func BenchmarkQueryLinkAtScale(b *testing.B) {
	eng := NewEngine()

	var braids []numeric.Phase
	var subjects []string
	var objects []string

	for i := 0; i < 200; i++ {
		s := fmt.Sprintf("S%d", i)
		o := fmt.Sprintf("O%d", i)
		subjects = append(subjects, s)
		objects = append(objects, o)

		braids = append(braids, eng.Inject(s, fmt.Sprintf("V%d", i%20), o))
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		idx := i % 200
		eng.QueryLink(braids[idx], subjects[idx], objects[idx])
	}
}
