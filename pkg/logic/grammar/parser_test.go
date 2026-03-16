package grammar

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
)

func TestLinguisticGrammar(t *testing.T) {
	calc := numeric.NewCalculus()

	cases := map[string]struct {
		Sentence          string
		ExpectedError     bool
		ErrorContains     string
		SetupFunc         func(p *ParserServer)
		ExpectedSubject   string
		ExpectedVerb      string
		ExpectedObject    string
		ExpectedSubjMods  []string
		ExpectedVerbMods  []string
		ExpectedObjMods   []string
		CalculateExpected func() numeric.Phase
	}{
		"basic_svo": {
			Sentence:      "fox jumped dog",
			ExpectedError: false,
			SetupFunc: func(p *ParserServer) {
				p.RegisterNoun("fox", "dog")
				p.RegisterVerb("jumped")
			},
			ExpectedSubject: "fox",
			ExpectedVerb:    "jumped",
			ExpectedObject:  "dog",
			CalculateExpected: func() numeric.Phase {
				// State = (State * 3) + NodePhase
				base := numeric.Phase(1)
				s1 := calc.Add(calc.Multiply(base, 3), calc.Sum("fox"))
				s2 := calc.Add(calc.Multiply(s1, 3), calc.Sum("jumped"))
				s3 := calc.Add(calc.Multiply(s2, 3), calc.Sum("dog"))
				return s3
			},
		},
		"svo_with_modifiers": {
			Sentence:      "quick brown fox jumped lazy dog",
			ExpectedError: false,
			SetupFunc: func(p *ParserServer) {
				p.RegisterNoun("fox", "dog")
				p.RegisterVerb("jumped")
				p.RegisterAdjective("quick", "brown", "lazy")
			},
			ExpectedSubject:  "fox",
			ExpectedVerb:     "jumped",
			ExpectedObject:   "dog",
			ExpectedSubjMods: []string{"quick", "brown"},
			ExpectedObjMods:  []string{"lazy"},
			CalculateExpected: func() numeric.Phase {
				// NodePhase = (NodePhase * 3) + ModPhase
				foxPhase := calc.Add(calc.Multiply(calc.Sum("fox"), 3), calc.Sum("quick"))
				foxPhase = calc.Add(calc.Multiply(foxPhase, 3), calc.Sum("brown"))

				jumpedPhase := calc.Sum("jumped")

				dogPhase := calc.Add(calc.Multiply(calc.Sum("dog"), 3), calc.Sum("lazy"))

				base := numeric.Phase(1)
				s1 := calc.Add(calc.Multiply(base, 3), foxPhase)
				s2 := calc.Add(calc.Multiply(s1, 3), jumpedPhase)
				s3 := calc.Add(calc.Multiply(s2, 3), dogPhase)
				return s3
			},
		},
		"non_commutative_validation": {
			Sentence:      "dog jumped fox",
			ExpectedError: false,
			SetupFunc: func(p *ParserServer) {
				p.RegisterNoun("fox", "dog")
				p.RegisterVerb("jumped")
			},
			ExpectedSubject: "dog",
			ExpectedVerb:    "jumped",
			ExpectedObject:  "fox",
			CalculateExpected: func() numeric.Phase {
				base := numeric.Phase(1)
				s1 := calc.Add(calc.Multiply(base, 3), calc.Sum("dog"))
				s2 := calc.Add(calc.Multiply(s1, 3), calc.Sum("jumped"))
				s3 := calc.Add(calc.Multiply(s2, 3), calc.Sum("fox"))
				return s3
			},
		},
		"unrecognized_entity": {
			Sentence:      "quick alien jumped dog",
			ExpectedError: true,
			ErrorContains: "unrecognized grammar entity",
			SetupFunc: func(p *ParserServer) {
				p.RegisterNoun("dog")
				p.RegisterVerb("jumped")
				p.RegisterAdjective("quick")
			},
			CalculateExpected: func() numeric.Phase { return 0 },
		},
		"structure_too_short": {
			Sentence:      "dog jumped",
			ExpectedError: true,
			ErrorContains: "requires at least S-V-O structure", // Fails initial length check
			SetupFunc: func(p *ParserServer) {
				p.RegisterNoun("dog")
				p.RegisterVerb("jumped")
			},
			CalculateExpected: func() numeric.Phase { return 0 },
		},
		"only_adjectives": {
			Sentence:      "quick brown lazy",
			ExpectedError: true,
			ErrorContains: "incomplete sentence structure", // State check catches this now!
			SetupFunc: func(p *ParserServer) {
				p.RegisterAdjective("quick", "brown", "lazy")
			},
			CalculateExpected: func() numeric.Phase { return 0 },
		},
		"trailing_adjectives": {
			Sentence:      "dog jumped fox quick",
			ExpectedError: true,
			ErrorContains: "trailing modifiers",
			SetupFunc: func(p *ParserServer) {
				p.RegisterNoun("fox", "dog")
				p.RegisterVerb("jumped")
				p.RegisterAdjective("quick")
			},
			CalculateExpected: func() numeric.Phase { return 0 },
		},
		"multiple_verbs": {
			Sentence:      "dog jumped ran fox",
			ExpectedError: true,
			ErrorContains: "unexpected verb",
			SetupFunc: func(p *ParserServer) {
				p.RegisterNoun("fox", "dog")
				p.RegisterVerb("jumped", "ran")
			},
			CalculateExpected: func() numeric.Phase { return 0 },
		},
	}

	for name, tc := range cases {
		Convey("Given case: "+name, t, func() {
			parser := NewParserServer(ParserWithContext(context.Background()))
			tc.SetupFunc(parser)

			ast, phase, err := parser.ParseSentence(tc.Sentence)

			if tc.ExpectedError {
				So(err, ShouldNotBeNil)
				if tc.ErrorContains != "" {
					So(err.Error(), ShouldContainSubstring, tc.ErrorContains)
				}
				So(ast, ShouldBeNil)
			} else {
				So(err, ShouldBeNil)
				So(ast, ShouldNotBeNil)

				// 1. Verify AST structural parsing
				So(ast.Subject.Entity, ShouldEqual, tc.ExpectedSubject)
				So(ast.Verb.Entity, ShouldEqual, tc.ExpectedVerb)
				So(ast.Object.Entity, ShouldEqual, tc.ExpectedObject)

				if len(tc.ExpectedSubjMods) > 0 {
					So(ast.Subject.Modifiers, ShouldResemble, tc.ExpectedSubjMods)
				} else {
					So(len(ast.Subject.Modifiers), ShouldEqual, 0)
				}

				if len(tc.ExpectedVerbMods) > 0 {
					So(ast.Verb.Modifiers, ShouldResemble, tc.ExpectedVerbMods)
				} else {
					So(len(ast.Verb.Modifiers), ShouldEqual, 0)
				}

				if len(tc.ExpectedObjMods) > 0 {
					So(ast.Object.Modifiers, ShouldResemble, tc.ExpectedObjMods)
				} else {
					So(len(ast.Object.Modifiers), ShouldEqual, 0)
				}

				// 2. Verify GF(257) Non-Commutative Phase
				expected := tc.CalculateExpected()
				So(phase, ShouldEqual, expected)
			}
		})
	}

	Convey("Validation of Commutativity Breach", t, func() {
		// This explicitly ensures that our structural math solved the flaw.
		parser := NewParserServer(ParserWithContext(context.Background()))
		parser.RegisterNoun("fox", "dog")
		parser.RegisterVerb("jumped")
		
		_, phase1, _ := parser.ParseSentence("fox jumped dog")
		_, phase2, _ := parser.ParseSentence("dog jumped fox")

		So(phase1, ShouldNotEqual, phase2) // MUST NOT BE EQUAL!
	})
}


