package semantic

import (
	"fmt"
	"math/rand"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
)

/*
computeExpectedBraid independently computes the S*L*O braid from raw strings
using only the Calculus API. This is the ground truth the Engine must match.
*/
func computeExpectedBraid(subject, link, object string) numeric.Phase {
	calc := numeric.NewCalculus()

	ps := calc.Sum(subject)
	pl := calc.Sum(link)
	po := calc.Sum(object)

	if ps == 0 || pl == 0 || po == 0 {
		return numeric.Phase(0)
	}

	return calc.Multiply(calc.Multiply(ps, pl), po)
}

/*
computeExpectedTarget independently cancels two components from a braid
to produce the expected target phase. Ground truth for query verification.
*/
func computeExpectedTarget(braid numeric.Phase, cancelA, cancelB string) (numeric.Phase, error) {
	calc := numeric.NewCalculus()

	invA, err := calc.Inverse(calc.Sum(cancelA))
	if err != nil {
		return 0, err
	}

	invB, err := calc.Inverse(calc.Sum(cancelB))
	if err != nil {
		return 0, err
	}

	return calc.Multiply(calc.Multiply(braid, invA), invB), nil
}

/*
generateString creates a random string with controlled length.
*/
func generateString(rng *rand.Rand, length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	buf := make([]byte, length)

	for idx := range buf {
		buf[idx] = charset[rng.Intn(len(charset))]
	}

	return string(buf)
}

/*
triple bundles a Subject-Link-Object fact with its injected braid.
*/
type triple struct {
	subj, link, obj string
	braid           numeric.Phase
}

/*
testCorpus defines a named collection of S-V-O triples for table-driven testing.
Each corpus stresses a different structural property of GF(257) algebraic cancellation.
*/
var testCorpora = map[string][]triple{
	// Natural language facts with shared subjects and links
	"natural_language": {
		{subj: "Sandra", link: "went_to", obj: "Kitchen"},
		{subj: "Sandra", link: "went_to", obj: "Garden"},
		{subj: "Sandra", link: "picked_up", obj: "Apple"},
		{subj: "John", link: "went_to", obj: "Kitchen"},
		{subj: "John", link: "dropped", obj: "Football"},
		{subj: "Mary", link: "is_in", obj: "Office"},
	},
	// Highly similar strings that stress the prime-weighted hash
	"near_anagrams": {
		{subj: "listen", link: "maps_to", obj: "enlist"},
		{subj: "silent", link: "maps_to", obj: "tinsel"},
		{subj: "cat", link: "sat_on", obj: "act"},
		{subj: "dog", link: "ran_to", obj: "god"},
		{subj: "tar", link: "covers", obj: "rat"},
		{subj: "evil", link: "versus", obj: "vile"},
	},
	// Single-character subjects to stress minimal entropy
	"minimal_entropy": {
		{subj: "a", link: "is", obj: "b"},
		{subj: "b", link: "is", obj: "c"},
		{subj: "c", link: "is", obj: "d"},
		{subj: "d", link: "is", obj: "e"},
		{subj: "e", link: "is", obj: "f"},
		{subj: "f", link: "is", obj: "a"},
	},
	// Long strings that cycle through every prime multiplier position
	"long_strings": {
		{subj: "the quick brown fox jumps over the lazy dog", link: "precedes", obj: "the quick brown cat leaps over the lazy frog"},
		{subj: "pneumonoultramicroscopicsilicovolcanoconiosis", link: "opposes", obj: "hippopotomonstrosesquippedaliophobia"},
		{subj: "AAAAAAAAAAAAAAAAAAAAAAAAAAAA", link: "contains", obj: "BBBBBBBBBBBBBBBBBBBBBBBBBBBB"},
	},
	// Numeric-looking strings
	"numeric_strings": {
		{subj: "192.168.1.1", link: "connected_to", obj: "10.0.0.1"},
		{subj: "3.14159265", link: "approximates", obj: "Pi"},
		{subj: "2.71828182", link: "approximates", obj: "Euler"},
		{subj: "0xFF", link: "equals", obj: "255"},
		{subj: "0b1010", link: "equals", obj: "10"},
	},
	// Unicode-like mixed content (ASCII subset)
	"mixed_content": {
		{subj: "user@host.com", link: "sent_to", obj: "admin@host.com"},
		{subj: "file:///tmp/a.txt", link: "links_to", obj: "file:///tmp/b.txt"},
		{subj: "key=value&foo=bar", link: "encodes", obj: "query_string"},
	},
}

func TestEngine(t *testing.T) {
	for corpusName, facts := range testCorpora {
		gc.Convey("Given corpus: "+corpusName, t, func() {
			eng := NewEngineServer()
			calc := numeric.NewCalculus()

			var injected []triple

			for _, fact := range facts {
				tt := triple{
					subj: fact.subj,
					link: fact.link,
					obj:  fact.obj,
				}

				tt.braid = eng.Inject(tt.subj, tt.link, tt.obj)
				injected = append(injected, tt)
			}

			gc.Convey(corpusName+": all braids should be non-zero and within GF(257)", func() {
				for _, tt := range injected {
					gc.So(tt.braid, gc.ShouldNotEqual, numeric.Phase(0))
					gc.So(uint32(tt.braid), gc.ShouldBeLessThan, numeric.FermatPrime)
				}
			})

			gc.Convey(corpusName+": every braid should match independently computed ground truth", func() {
				for _, tt := range injected {
					expected := computeExpectedBraid(tt.subj, tt.link, tt.obj)
					gc.So(tt.braid, gc.ShouldEqual, expected)
				}
			})

			gc.Convey(corpusName+": every fact should be stored with correct fields", func() {
				for idx, tt := range injected {
					gc.So(eng.facts[idx].Subject, gc.ShouldEqual, tt.subj)
					gc.So(eng.facts[idx].Link, gc.ShouldEqual, tt.link)
					gc.So(eng.facts[idx].Object, gc.ShouldEqual, tt.obj)
					gc.So(eng.facts[idx].Phase, gc.ShouldEqual, tt.braid)
				}
			})

			gc.Convey(corpusName+": QueryObject should recover the exact object for every fact", func() {
				for _, tt := range injected {
					name, phase, err := eng.QueryObject(tt.braid, tt.subj, tt.link)
					gc.So(err, gc.ShouldBeNil)
					gc.So(name, gc.ShouldEqual, tt.obj)
					gc.So(phase, gc.ShouldEqual, calc.Sum(tt.obj))
				}
			})

			gc.Convey(corpusName+": QueryObject target should match independently computed cancellation", func() {
				for _, tt := range injected {
					_, phase, err := eng.QueryObject(tt.braid, tt.subj, tt.link)
					gc.So(err, gc.ShouldBeNil)

					expected, calcErr := computeExpectedTarget(tt.braid, tt.subj, tt.link)
					gc.So(calcErr, gc.ShouldBeNil)
					gc.So(phase, gc.ShouldEqual, expected)
				}
			})

			gc.Convey(corpusName+": QuerySubject should recover the exact subject for every fact", func() {
				for _, tt := range injected {
					name, phase, err := eng.QuerySubject(tt.braid, tt.link, tt.obj)
					gc.So(err, gc.ShouldBeNil)
					gc.So(name, gc.ShouldEqual, tt.subj)
					gc.So(phase, gc.ShouldEqual, calc.Sum(tt.subj))
				}
			})

			gc.Convey(corpusName+": QuerySubject target should match independently computed cancellation", func() {
				for _, tt := range injected {
					_, phase, err := eng.QuerySubject(tt.braid, tt.link, tt.obj)
					gc.So(err, gc.ShouldBeNil)

					expected, calcErr := computeExpectedTarget(tt.braid, tt.link, tt.obj)
					gc.So(calcErr, gc.ShouldBeNil)
					gc.So(phase, gc.ShouldEqual, expected)
				}
			})

			gc.Convey(corpusName+": QueryLink should recover the exact link for every fact", func() {
				for _, tt := range injected {
					name, phase, err := eng.QueryLink(tt.braid, tt.subj, tt.obj)
					gc.So(err, gc.ShouldBeNil)
					gc.So(name, gc.ShouldEqual, tt.link)
					gc.So(phase, gc.ShouldEqual, calc.Sum(tt.link))
				}
			})

			gc.Convey(corpusName+": the full query triad should be self-consistent for every fact", func() {
				for _, tt := range injected {
					obj, po, errO := eng.QueryObject(tt.braid, tt.subj, tt.link)
					subj, ps, errS := eng.QuerySubject(tt.braid, tt.link, tt.obj)
					link, pl, errL := eng.QueryLink(tt.braid, tt.subj, tt.obj)

					gc.So(errO, gc.ShouldBeNil)
					gc.So(errS, gc.ShouldBeNil)
					gc.So(errL, gc.ShouldBeNil)

					// Reconstruct braid from recovered phases — must equal original
					reconstructed := calc.Multiply(calc.Multiply(ps, pl), po)
					gc.So(reconstructed, gc.ShouldEqual, tt.braid)

					// Recovered strings must match originals
					gc.So(obj, gc.ShouldEqual, tt.obj)
					gc.So(subj, gc.ShouldEqual, tt.subj)
					gc.So(link, gc.ShouldEqual, tt.link)
				}
			})

			gc.Convey(corpusName+": phaseIndex should contain every object and subject", func() {
				for _, tt := range injected {
					po := calc.Sum(tt.obj)
					ps := calc.Sum(tt.subj)

					gc.So(len(eng.phaseIndex[po]), gc.ShouldBeGreaterThan, 0)
					gc.So(len(eng.phaseIndex[ps]), gc.ShouldBeGreaterThan, 0)
				}
			})

			gc.Convey(corpusName+": braidIndex should reference every injected fact", func() {
				for factIdx, tt := range injected {
					bucket := eng.braidIndex[tt.braid]
					found := false

					for _, storedIdx := range bucket {
						if storedIdx == factIdx {
							found = true
							break
						}
					}

					gc.So(found, gc.ShouldBeTrue)
				}
			})
		})
	}
}

func TestZeroComponentRejection(t *testing.T) {
	gc.Convey("Given empty-string components", t, func() {
		eng := NewEngineServer()

		gc.Convey("Inject should reject empty subject, link, or object", func() {
			gc.So(eng.Inject("", "is_on", "Mat"), gc.ShouldEqual, numeric.Phase(0))
			gc.So(eng.Inject("Cat", "", "Mat"), gc.ShouldEqual, numeric.Phase(0))
			gc.So(eng.Inject("Cat", "is_on", ""), gc.ShouldEqual, numeric.Phase(0))
			gc.So(len(eng.facts), gc.ShouldEqual, 0)
		})

		gc.Convey("InjectLabel should reject empty components or zero label", func() {
			gc.So(eng.InjectLabel("", "is_on", "Mat", 42), gc.ShouldEqual, numeric.Phase(0))
			gc.So(eng.InjectLabel("Cat", "", "Mat", 42), gc.ShouldEqual, numeric.Phase(0))
			gc.So(eng.InjectLabel("Cat", "is_on", "", 42), gc.ShouldEqual, numeric.Phase(0))
			gc.So(eng.InjectLabel("Cat", "is_on", "Mat", 0), gc.ShouldEqual, numeric.Phase(0))
			gc.So(len(eng.facts), gc.ShouldEqual, 0)
		})

		gc.Convey("QueryObject should return error on zero-phase subject or link", func() {
			braid := eng.Inject("Cat", "is_on", "Mat")
			_, _, err := eng.QueryObject(braid, "", "is_on")
			gc.So(err, gc.ShouldNotBeNil)

			_, _, err = eng.QueryObject(braid, "Cat", "")
			gc.So(err, gc.ShouldNotBeNil)
		})

		gc.Convey("QuerySubject should return error on zero-phase link or object", func() {
			braid := eng.Inject("Cat", "is_on", "Mat")
			_, _, err := eng.QuerySubject(braid, "", "Mat")
			gc.So(err, gc.ShouldNotBeNil)

			_, _, err = eng.QuerySubject(braid, "is_on", "")
			gc.So(err, gc.ShouldNotBeNil)
		})

		gc.Convey("DeBraidFact should return ErrZeroInverse on empty components", func() {
			braid := eng.Inject("Cat", "is_on", "Mat")
			_, err := eng.DeBraidFact(braid, "", "is_on", "Mat")
			gc.So(err, gc.ShouldEqual, numeric.ErrZeroInverse)

			_, err = eng.DeBraidFact(braid, "Cat", "", "Mat")
			gc.So(err, gc.ShouldEqual, numeric.ErrZeroInverse)

			_, err = eng.DeBraidFact(braid, "Cat", "is_on", "")
			gc.So(err, gc.ShouldEqual, numeric.ErrZeroInverse)
		})
	})
}

func TestAlgebraicIdentities(t *testing.T) {
	gc.Convey("Given the GF(257) algebraic cancellation identity", t, func() {
		calc := numeric.NewCalculus()

		gc.Convey("S*L*O * inv(S) * inv(L) should equal O for 200 random facts", func() {
			rng := rand.New(rand.NewSource(42))

			for range 200 {
				subj := generateString(rng, rng.Intn(20)+3)
				link := generateString(rng, rng.Intn(10)+3)
				obj := generateString(rng, rng.Intn(20)+3)

				ps := calc.Sum(subj)
				pl := calc.Sum(link)
				po := calc.Sum(obj)

				if ps == 0 || pl == 0 || po == 0 {
					continue
				}

				braid := calc.Multiply(calc.Multiply(ps, pl), po)

				invS, err := calc.Inverse(ps)
				gc.So(err, gc.ShouldBeNil)

				invL, err := calc.Inverse(pl)
				gc.So(err, gc.ShouldBeNil)

				result := calc.Multiply(calc.Multiply(braid, invS), invL)
				gc.So(result, gc.ShouldEqual, po)
			}
		})

		gc.Convey("Cancellation should work symmetrically on all three axes", func() {
			rng := rand.New(rand.NewSource(99))

			for range 100 {
				subj := generateString(rng, rng.Intn(15)+3)
				link := generateString(rng, rng.Intn(8)+3)
				obj := generateString(rng, rng.Intn(15)+3)

				ps := calc.Sum(subj)
				pl := calc.Sum(link)
				po := calc.Sum(obj)

				if ps == 0 || pl == 0 || po == 0 {
					continue
				}

				braid := calc.Multiply(calc.Multiply(ps, pl), po)

				// Cancel to Object
				invS, _ := calc.Inverse(ps)
				invL, _ := calc.Inverse(pl)
				gc.So(calc.Multiply(calc.Multiply(braid, invS), invL), gc.ShouldEqual, po)

				// Cancel to Subject
				invO, _ := calc.Inverse(po)
				gc.So(calc.Multiply(calc.Multiply(braid, invL), invO), gc.ShouldEqual, ps)

				// Cancel to Link
				gc.So(calc.Multiply(calc.Multiply(braid, invS), invO), gc.ShouldEqual, pl)
			}
		})
	})
}

func TestMultiplication_Commutativity(t *testing.T) {
	gc.Convey("Given GF(257) multiplication is commutative", t, func() {
		eng := NewEngineServer()
		calc := eng.calc

		gc.Convey("Swapping S and O with the same link should produce the same braid", func() {
			for _, corpus := range testCorpora {
				for _, fact := range corpus {
					ps := calc.Sum(fact.subj)
					pl := calc.Sum(fact.link)
					po := calc.Sum(fact.obj)

					fwd := calc.Multiply(calc.Multiply(ps, pl), po)
					rev := calc.Multiply(calc.Multiply(po, pl), ps)

					gc.So(fwd, gc.ShouldEqual, rev)
				}
			}
		})

		gc.Convey("Prime-weighted hash should distinguish anagrams despite commutativity", func() {
			gc.So(calc.Sum("listen"), gc.ShouldNotEqual, calc.Sum("silent"))
			gc.So(calc.Sum("cat"), gc.ShouldNotEqual, calc.Sum("act"))
			gc.So(calc.Sum("abc"), gc.ShouldNotEqual, calc.Sum("cba"))
			gc.So(calc.Sum("dog"), gc.ShouldNotEqual, calc.Sum("god"))
		})
	})
}

func TestMergeAlgebra(t *testing.T) {
	gc.Convey("Given Merge operates via GF(257) addition", t, func() {
		eng := NewEngineServer()
		calc := eng.calc

		phaseA := eng.Inject("Cat", "is_on", "Mat")
		phaseB := eng.Inject("Dog", "is_in", "Yard")
		phaseC := eng.Inject("Bird", "flew", "Sky")

		gc.Convey("Merge of empty should be zero (additive identity)", func() {
			gc.So(eng.Merge([]numeric.Phase{}), gc.ShouldEqual, numeric.Phase(0))
		})

		gc.Convey("Merge of single should return that phase", func() {
			gc.So(eng.Merge([]numeric.Phase{phaseA}), gc.ShouldEqual, phaseA)
		})

		gc.Convey("Merge should be commutative: A+B == B+A", func() {
			gc.So(
				eng.Merge([]numeric.Phase{phaseA, phaseB}),
				gc.ShouldEqual,
				eng.Merge([]numeric.Phase{phaseB, phaseA}),
			)
		})

		gc.Convey("Merge should be associative: (A+B)+C == A+(B+C)", func() {
			ab_c := eng.Merge([]numeric.Phase{calc.Add(phaseA, phaseB), phaseC})
			a_bc := eng.Merge([]numeric.Phase{phaseA, calc.Add(phaseB, phaseC)})
			gc.So(ab_c, gc.ShouldEqual, a_bc)
		})

		gc.Convey("Merge with additive inverse should cancel to zero", func() {
			anti := calc.Subtract(numeric.Phase(0), phaseA)
			gc.So(eng.Merge([]numeric.Phase{phaseA, anti}), gc.ShouldEqual, numeric.Phase(0))
		})

		gc.Convey("All merged values should remain within GF(257)", func() {
			var phases []numeric.Phase

			for idx := range 50 {
				phases = append(phases, eng.Inject(
					fmt.Sprintf("S%d", idx),
					fmt.Sprintf("V%d", idx),
					fmt.Sprintf("O%d", idx),
				))
			}

			merged := eng.Merge(phases)
			gc.So(uint32(merged), gc.ShouldBeLessThan, numeric.FermatPrime)
		})
	})
}

func TestMultiTonalNoise(t *testing.T) {
	gc.Convey("Given 6 merged contexts in a multi-tonal braid", t, func() {
		eng := NewEngineServer()
		calc := eng.calc

		var phases []numeric.Phase

		for idx := range 6 {
			eng.Inject(
				fmt.Sprintf("Subj_%d", idx),
				fmt.Sprintf("Link_%d", idx),
				fmt.Sprintf("Obj_%d", idx),
			)

			ps := calc.Sum(fmt.Sprintf("Subj_%d", idx))
			pl := calc.Sum(fmt.Sprintf("Link_%d", idx))
			po := calc.Sum(fmt.Sprintf("Obj_%d", idx))

			phases = append(phases, calc.Multiply(calc.Multiply(ps, pl), po))
		}

		merged := eng.Merge(phases)

		gc.Convey("Merged braid from superposition should NOT resolve cleanly (noise floor)", func() {
			loc, _, err := eng.QueryObject(merged, "Subj_0", "Link_0")
			gc.So(err, gc.ShouldBeNil)
			gc.So(loc, gc.ShouldNotEqual, "Obj_0")
		})

		gc.Convey("Each individual braid should still resolve exactly", func() {
			for idx := range 6 {
				name, _, err := eng.QueryObject(
					phases[idx],
					fmt.Sprintf("Subj_%d", idx),
					fmt.Sprintf("Link_%d", idx),
				)
				gc.So(err, gc.ShouldBeNil)
				gc.So(name, gc.ShouldEqual, fmt.Sprintf("Obj_%d", idx))
			}
		})
	})
}

func TestMassInjectionAndQuery(t *testing.T) {
	gc.Convey("Given 10,000 random facts", t, func() {
		eng := NewEngineServer()
		rng := rand.New(rand.NewSource(42))

		var injected []triple

		for range 10000 {
			tt := triple{
				subj: generateString(rng, rng.Intn(10)+5),
				link: generateString(rng, rng.Intn(5)+3),
				obj:  generateString(rng, rng.Intn(10)+5),
			}

			tt.braid = eng.Inject(tt.subj, tt.link, tt.obj)

			if tt.braid != 0 {
				injected = append(injected, tt)
			}
		}

		gc.So(len(injected), gc.ShouldBeGreaterThan, 9500)

		gc.Convey("200 random QueryObject calls should return exact objects", func() {
			for range 200 {
				idx := rng.Intn(len(injected))
				tt := injected[idx]

				name, _, err := eng.QueryObject(tt.braid, tt.subj, tt.link)
				gc.So(err, gc.ShouldBeNil)
				gc.So(name, gc.ShouldEqual, tt.obj)
			}
		})

		gc.Convey("200 random QuerySubject calls should return exact subjects", func() {
			for range 200 {
				idx := rng.Intn(len(injected))
				tt := injected[idx]

				name, _, err := eng.QuerySubject(tt.braid, tt.link, tt.obj)
				gc.So(err, gc.ShouldBeNil)
				gc.So(name, gc.ShouldEqual, tt.subj)
			}
		})

		gc.Convey("Every returned phase should match independently computed braid", func() {
			for range 100 {
				idx := rng.Intn(len(injected))
				tt := injected[idx]

				expected := computeExpectedBraid(tt.subj, tt.link, tt.obj)
				gc.So(tt.braid, gc.ShouldEqual, expected)
			}
		})
	})
}

func TestFunctionalOptions(t *testing.T) {
	gc.Convey("Given EngineWithFact option", t, func() {
		gc.Convey("It should pre-load a fact that is immediately queryable", func() {
			eng := NewEngineServer(EngineWithFact("Cat", "is_on", "Mat"))

			gc.So(len(eng.facts), gc.ShouldEqual, 1)

			name, _, err := eng.QueryObject(eng.facts[0].Phase, "Cat", "is_on")
			gc.So(err, gc.ShouldBeNil)
			gc.So(name, gc.ShouldEqual, "Mat")
		})

		gc.Convey("Multiple options should chain", func() {
			eng := NewEngineServer(
				EngineWithFact("Cat", "is_on", "Mat"),
				EngineWithFact("Dog", "is_in", "Yard"),
			)
			gc.So(len(eng.facts), gc.ShouldEqual, 2)
		})
	})

	gc.Convey("Given EngineWithTemporalFact option", t, func() {
		gc.Convey("It should pre-load a temporal fact", func() {
			eng := NewEngineServer(EngineWithTemporalFact("Sandra", "is_in", "Garden", 5))

			gc.So(len(eng.facts), gc.ShouldEqual, 1)
			gc.So(eng.facts[0].Temporal, gc.ShouldEqual, numeric.Phase(5))
			gc.So(eng.facts[0].Phase, gc.ShouldNotEqual, numeric.Phase(0))
		})
	})
}

func TestInjectLabel(t *testing.T) {
	gc.Convey("Given cross-modal label injection", t, func() {
		eng := NewEngineServer()
		calc := eng.calc

		gc.Convey("Label should modulate the braid multiplicatively", func() {
			labelPhase := numeric.Phase(42)
			modulated := eng.InjectLabel("Cat", "is_on", "Mat", labelPhase)

			plain := computeExpectedBraid("Cat", "is_on", "Mat")
			expected := calc.Multiply(plain, labelPhase)

			gc.So(modulated, gc.ShouldEqual, expected)
			gc.So(modulated, gc.ShouldNotEqual, plain)
		})

		gc.Convey("Different labels on the same S-V-O should yield different braids", func() {
			braidA := eng.InjectLabel("Cat", "is_on", "Mat", numeric.Phase(10))
			braidB := eng.InjectLabel("Cat", "is_on", "Mat", numeric.Phase(20))
			gc.So(braidA, gc.ShouldNotEqual, braidB)
		})

		gc.Convey("Label should be stored on the fact", func() {
			eng2 := NewEngineServer()
			eng2.InjectLabel("Cat", "is_on", "Mat", numeric.Phase(99))
			gc.So(eng2.facts[0].Label, gc.ShouldEqual, numeric.Phase(99))
		})
	})
}

func TestInterference(t *testing.T) {
	gc.Convey("Given the destructive interference detector", t, func() {
		eng := NewEngineServer()

		gc.Convey("Exact additive inverses should interfere", func() {
			pos := eng.Inject("Cat", "is_on", "Mat")
			neg := eng.InjectNegation("Cat", "is_on", "Mat")
			gc.So(eng.Interference(pos, neg), gc.ShouldBeTrue)
		})

		gc.Convey("Unrelated phases should not interfere", func() {
			phaseA := eng.Inject("Cat", "is_on", "Mat")
			phaseB := eng.Inject("Dog", "is_in", "Yard")
			gc.So(eng.Interference(phaseA, phaseB), gc.ShouldBeFalse)
		})

		gc.Convey("Additive inverse sum should be exactly zero", func() {
			pos := eng.Inject("Cat", "is_on", "Mat")
			neg := eng.InjectNegation("Cat", "is_on", "Mat")
			gc.So(eng.calc.Add(pos, neg), gc.ShouldEqual, numeric.Phase(0))
		})
	})
}

func TestDiff(t *testing.T) {
	gc.Convey("Given the modular distance function", t, func() {
		eng := NewEngineServer()

		gc.Convey("Identical phases should have zero distance", func() {
			gc.So(eng.diff(numeric.Phase(10), numeric.Phase(10)), gc.ShouldEqual, uint32(0))
			gc.So(eng.diff(numeric.Phase(0), numeric.Phase(0)), gc.ShouldEqual, uint32(0))
		})

		gc.Convey("Distance should wrap around mod FermatPrime and take the shorter path", func() {
			gc.So(eng.diff(numeric.Phase(1), numeric.Phase(256)), gc.ShouldEqual, uint32(2))
			gc.So(eng.diff(numeric.Phase(256), numeric.Phase(1)), gc.ShouldEqual, uint32(2))
		})

		gc.Convey("Distance should be symmetric", func() {
			gc.So(
				eng.diff(numeric.Phase(10), numeric.Phase(200)),
				gc.ShouldEqual,
				eng.diff(numeric.Phase(200), numeric.Phase(10)),
			)
		})

		gc.Convey("Adjacent phases should have distance 1", func() {
			gc.So(eng.diff(numeric.Phase(100), numeric.Phase(101)), gc.ShouldEqual, uint32(1))
		})
	})
}

func TestResonate(t *testing.T) {
	gc.Convey("Given an engine with indexed facts", t, func() {
		eng := NewEngineServer()
		eng.Inject("Cat", "is_on", "Mat")
		eng.Inject("Dog", "is_in", "Yard")

		gc.Convey("Exact object phase should return the object string", func() {
			target := eng.calc.Sum("Mat")
			name, phase := eng.Resonate(target)
			gc.So(name, gc.ShouldEqual, "Mat")
			gc.So(phase, gc.ShouldEqual, target)
		})

		gc.Convey("Exact subject phase should return the subject string", func() {
			target := eng.calc.Sum("Cat")
			name, phase := eng.Resonate(target)
			gc.So(name, gc.ShouldEqual, "Cat")
			gc.So(phase, gc.ShouldEqual, target)
		})

		gc.Convey("A phase far from all stored values should return empty", func() {
			found := false

			for candidate := uint32(1); candidate < numeric.FermatPrime; candidate++ {
				name, _ := eng.Resonate(numeric.Phase(candidate))
				if name == "" {
					found = true
					break
				}
			}

			gc.So(found, gc.ShouldBeTrue)
		})
	})
}

// --- Benchmarks ---

func BenchmarkInject(b *testing.B) {
	eng := NewEngineServer()
	b.ResetTimer()

	for iter := 0; iter < b.N; iter++ {
		eng.Inject(
			fmt.Sprintf("S%d", iter),
			fmt.Sprintf("V%d", iter%20),
			fmt.Sprintf("O%d", iter),
		)
	}
}

func BenchmarkQueryObject(b *testing.B) {
	eng := NewEngineServer()

	var braids []numeric.Phase
	var subjects []string
	var links []string

	for idx := 0; idx < 500; idx++ {
		subj := fmt.Sprintf("S%d", idx)
		link := fmt.Sprintf("V%d", idx%20)
		subjects = append(subjects, subj)
		links = append(links, link)
		braids = append(braids, eng.Inject(subj, link, fmt.Sprintf("O%d", idx)))
	}

	b.ResetTimer()

	for iter := 0; iter < b.N; iter++ {
		pick := iter % 500
		eng.QueryObject(braids[pick], subjects[pick], links[pick])
	}
}

func BenchmarkQuerySubject(b *testing.B) {
	eng := NewEngineServer()

	var braids []numeric.Phase
	var links []string
	var objects []string

	for idx := 0; idx < 500; idx++ {
		link := fmt.Sprintf("V%d", idx%20)
		obj := fmt.Sprintf("O%d", idx)
		links = append(links, link)
		objects = append(objects, obj)
		braids = append(braids, eng.Inject(fmt.Sprintf("S%d", idx), link, obj))
	}

	b.ResetTimer()

	for iter := 0; iter < b.N; iter++ {
		pick := iter % 500
		eng.QuerySubject(braids[pick], links[pick], objects[pick])
	}
}

func BenchmarkQueryLink(b *testing.B) {
	eng := NewEngineServer()

	var braids []numeric.Phase
	var subjects []string
	var objects []string

	for idx := 0; idx < 500; idx++ {
		subj := fmt.Sprintf("S%d", idx)
		obj := fmt.Sprintf("O%d", idx)
		subjects = append(subjects, subj)
		objects = append(objects, obj)
		braids = append(braids, eng.Inject(subj, fmt.Sprintf("V%d", idx%20), obj))
	}

	b.ResetTimer()

	for iter := 0; iter < b.N; iter++ {
		pick := iter % 500
		eng.QueryLink(braids[pick], subjects[pick], objects[pick])
	}
}

func BenchmarkMerge(b *testing.B) {
	eng := NewEngineServer()

	var phases []numeric.Phase

	for idx := 0; idx < 100; idx++ {
		phases = append(phases, eng.Inject(
			fmt.Sprintf("S%d", idx),
			fmt.Sprintf("V%d", idx),
			fmt.Sprintf("O%d", idx),
		))
	}

	b.ResetTimer()

	for iter := 0; iter < b.N; iter++ {
		eng.Merge(phases)
	}
}

func BenchmarkResonate(b *testing.B) {
	eng := NewEngineServer()

	for idx := 0; idx < 1000; idx++ {
		eng.Inject(
			fmt.Sprintf("S%d", idx),
			fmt.Sprintf("V%d", idx%30),
			fmt.Sprintf("O%d", idx),
		)
	}

	targets := make([]numeric.Phase, 1000)

	for idx := range targets {
		targets[idx] = eng.calc.Sum(fmt.Sprintf("O%d", idx))
	}

	b.ResetTimer()

	for iter := 0; iter < b.N; iter++ {
		eng.Resonate(targets[iter%1000])
	}
}

func BenchmarkModularInverse(b *testing.B) {
	eng := NewEngineServer()
	b.ResetTimer()

	for iter := 0; iter < b.N; iter++ {
		eng.calc.Inverse(numeric.Phase((iter % 256) + 1))
	}
}

func BenchmarkInterference(b *testing.B) {
	eng := NewEngineServer()

	var posPhases []numeric.Phase
	var negPhases []numeric.Phase

	for idx := 0; idx < 100; idx++ {
		posPhases = append(posPhases, eng.Inject(
			fmt.Sprintf("S%d", idx), "rel", fmt.Sprintf("O%d", idx),
		))
		negPhases = append(negPhases, eng.InjectNegation(
			fmt.Sprintf("S%d", idx), "rel", fmt.Sprintf("O%d", idx),
		))
	}

	b.ResetTimer()

	for iter := 0; iter < b.N; iter++ {
		pick := iter % 100
		eng.Interference(posPhases[pick], negPhases[pick])
	}
}
