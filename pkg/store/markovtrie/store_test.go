package markovtrie

import (
	"math"
	"math/rand"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func buildSequenceStore() *Store {
	store := NewStore(WithDecayFactor(1), WithRandomSource(rand.NewSource(7)))

	initialData := map[string][]string{
		"Truck": {
			"blue_cab_big_wheel",
			"blue_cab_flat_bed",
			"white_cab_big_wheel",
			"red_cab_flat_bed",
			"heavy_duty_truck",
			"diesel_engine_roar",
		},
		"Car": {
			"blue_hood_small_tire",
			"red_hood_small_tire",
			"blue_hood_spoiler",
			"white_hood_small_tire",
			"fast_sports_car",
			"electric_sedan",
		},
		"Bike": {
			"red_tank_two_wheel",
			"black_tank_two_wheel",
			"blue_tank_two_wheel",
			"mountain_bike_tires",
			"carbon_fiber_frame",
		},
	}

	for _, label := range []string{"Truck", "Car", "Bike"} {
		for _, sequence := range initialData[label] {
			store.Insert(sequence, label)
		}
	}

	return store
}

func TestNewSequenceStore(t *testing.T) {
	Convey("Given a new sequence store", t, func() {
		store := NewStore()

		Convey("It should initialize token-level defaults", func() {
			So(store.root, ShouldNotBeNil)
			So(store.root.ID, ShouldEqual, "root")
			So(store.nodeCount, ShouldEqual, 1)
			So(store.decayFactor, ShouldEqual, defaultDecayFactor)
			So(store.endToken, ShouldEqual, defaultEndToken)
			So(store.currentStep, ShouldEqual, 0)
		})
	})
}

func TestTokenize(t *testing.T) {
	Convey("Given a token sequence store", t, func() {
		store := NewStore()

		Convey("Tokenize should preserve content and separator runs", func() {
			tokens := store.Tokenize("blue_cab  fast")

			So(tokens, ShouldResemble, []string{"blue", "_", "cab", "  ", "fast"})
		})
	})
}

func TestInsert(t *testing.T) {
	Convey("Given an empty sequence store", t, func() {
		store := NewStore(WithDecayFactor(1))

		Convey("Insert should train suffix paths and class totals", func() {
			store.Insert("truck", "Vehicle")

			So(store.labels, ShouldResemble, []string{"Vehicle"})
			So(store.ClassTotals["Vehicle"], ShouldEqual, 1)
			So(store.currentStep, ShouldEqual, 1)
			So(store.nodeCount, ShouldEqual, 4)
			So(store.root.TotalVisits, ShouldEqual, 2)
			So(store.root.Children["truck"].ClassCounts["Vehicle"], ShouldEqual, 1)
			So(store.root.Children["$"].ClassCounts["Vehicle"], ShouldEqual, 1)
		})

		Convey("Insert should decay untouched nodes lazily across steps", func() {
			store = NewStore(WithDecayFactor(0.5))
			store.Insert("truck", "Vehicle")
			store.Insert("car", "Auto")

			So(store.ClassTotals["Vehicle"], ShouldEqual, 0.5)
			So(store.ClassTotals["Auto"], ShouldEqual, 1)
			So(store.EffectiveCount(store.root.Children["truck"], "Vehicle"), ShouldEqual, 0.5)
		})
	})
}

func TestMatchContext(t *testing.T) {
	Convey("Given a trained sequence store", t, func() {
		store := NewStore(WithDecayFactor(1))
		store.Insert("blue_cab", "Truck")

		Convey("MatchContext should return the longest token suffix that exists", func() {
			match := store.MatchContext("noise blue_cab")

			So(match.ActiveContext, ShouldEqual, "blue_cab")
			So(match.ActiveTokens, ShouldResemble, []string{"blue", "_", "cab"})
			So(match.Node.Token, ShouldEqual, "cab")
		})

		Convey("MatchContext should tolerate one-token fuzzy edits", func() {
			match := store.MatchContext("blue_cxb")

			So(match.ActiveContext, ShouldEqual, "blue_cab")
			So(match.ActiveTokens, ShouldResemble, []string{"blue", "_", "cab"})
		})
	})
}

func TestSimilarity(t *testing.T) {
	Convey("Given learned co-occurrence rows", t, func() {
		store := NewStore(WithDecayFactor(1))
		store.Insert("fast_sports_car", "Car")
		store.Insert("quick_sports_auto", "Car")

		Convey("Similarity should return cosine overlap between co-occurrence rows", func() {
			So(store.Similarity("fast", "quick"), ShouldBeGreaterThan, 0)
			So(store.Similarity("fast", "fast"), ShouldEqual, 1)
		})
	})
}

func TestEditDistance(t *testing.T) {
	Convey("Given two tokens", t, func() {
		store := NewStore()

		Convey("EditDistance should compute Levenshtein distance", func() {
			So(store.EditDistance("cab", "cxb"), ShouldEqual, 1)
			So(store.EditDistance("cab", "truck"), ShouldEqual, 5)
		})
	})
}

func TestSemanticEquivalent(t *testing.T) {
	Convey("Given a trained sequence store", t, func() {
		store := buildSequenceStore()

		Convey("SemanticEquivalent should use edit-distance matches before co-occurrence fallback", func() {
			match := store.SemanticEquivalent("cabb")

			So(match.Original, ShouldEqual, "cabb")
			So(match.Mapped, ShouldEqual, "cab")
			So(match.Similarity, ShouldEqual, defaultEditSimilarity)
		})

		Convey("SemanticEquivalent should keep unknown tokens unchanged when nothing matches", func() {
			match := store.SemanticEquivalent("zeppelin")

			So(match.Original, ShouldEqual, "zeppelin")
			So(match.Mapped, ShouldEqual, "zeppelin")
			So(match.Similarity, ShouldEqual, 1)
		})
	})
}

func TestAttentionContext(t *testing.T) {
	Convey("Given a trained sequence store", t, func() {
		store := buildSequenceStore()

		Convey("AttentionContext should map only content tokens", func() {
			matches := store.AttentionContext("cabb truck")

			So(len(matches), ShouldEqual, 2)
			So(matches[0].Mapped, ShouldEqual, "cab")
			So(matches[1].Mapped, ShouldEqual, "truck")
		})
	})
}

func TestInterpolatedProbabilities(t *testing.T) {
	Convey("Given a populated sequence store", t, func() {
		store := buildSequenceStore()

		Convey("InterpolatedProbabilities should assign mass to plausible next tokens", func() {
			probabilities := store.InterpolatedProbabilities("blue_", "Truck")

			So(probabilities["cab"], ShouldBeGreaterThan, 0)
			So(probabilities["hood"], ShouldBeLessThan, probabilities["cab"])
		})
	})
}

func TestClassify(t *testing.T) {
	Convey("Given a populated sequence store", t, func() {
		store := buildSequenceStore()

		Convey("Classify should normalize label posteriors to percentages", func() {
			scores := store.Classify("blue_cab")

			total := 0.0
			for _, score := range scores {
				total += score
			}

			So(math.Abs(total-100) < 1e-9, ShouldBeTrue)
			So(scores["Truck"], ShouldBeGreaterThan, scores["Car"])
			So(scores["Truck"], ShouldBeGreaterThan, scores["Bike"])
		})

		Convey("Classify should prefer the class with matching token history", func() {
			scores := store.Classify("black_tank")

			So(scores["Bike"], ShouldBeGreaterThan, scores["Car"])
			So(scores["Bike"], ShouldBeGreaterThan, scores["Truck"])
		})

		Convey("ClassifyDetailed should mirror Classify scores and expose token traces", func() {
			detailedScores, contributions := store.ClassifyDetailed("blue")
			plainScores := store.Classify("blue")

			So(math.Abs(detailedScores["Truck"]-plainScores["Truck"]) < 1e-9, ShouldBeTrue)
			So(len(contributions["Truck"]), ShouldBeGreaterThan, 1)
			So(contributions["Truck"][0].Token, ShouldEqual, "PRIOR")
		})
	})
}

func TestWordTokensOnlyTokenizer(t *testing.T) {
	Convey("Given a store with demo-style word boundaries", t, func() {
		store := NewStore(WithWordTokensOnly())

		Convey("Tokenize should omit standalone underscore tokens", func() {
			So(store.Tokenize("blue_cab"), ShouldResemble, []string{"blue", "cab"})
			So(store.Tokenize("blue cab"), ShouldResemble, []string{"blue", "cab"})
		})
	})
}

func TestSurprisalSeries(t *testing.T) {
	Convey("Given a populated sequence store", t, func() {
		store := buildSequenceStore()

		Convey("SurprisalSeries should return one value per token", func() {
			surprisals := store.SurprisalSeries("blue_cab")

			So(len(surprisals), ShouldEqual, 3)
			So(surprisals[0].Token, ShouldEqual, "blue")
			So(surprisals[0].Bits, ShouldBeGreaterThanOrEqualTo, 0)
		})

		Convey("SurprisalSeries should smooth unseen tokens", func() {
			surprisals := store.SurprisalSeries("zeppelin")

			So(len(surprisals), ShouldEqual, 1)
			So(math.IsInf(surprisals[0].Bits, 0), ShouldBeFalse)
			So(math.IsNaN(surprisals[0].Bits), ShouldBeFalse)
		})
	})
}

func TestNextProbabilities(t *testing.T) {
	Convey("Given a populated sequence store", t, func() {
		store := buildSequenceStore()

		Convey("NextProbabilities should return a normalized distribution", func() {
			internal := store.nextProbabilitiesFromTokens([]string{"blue", "_"}, "Truck", 0)
			public := store.NextProbabilities("blue_", "Truck", 0)

			So(len(public), ShouldEqual, len(internal))
			So(public[0].Token, ShouldEqual, internal[0].Token)

			total := 0.0
			for _, probability := range public {
				total += probability.Probability
			}

			So(len(public), ShouldBeGreaterThan, 0)
			So(public[0].Token, ShouldEqual, "cab")
			So(public[0].Probability, ShouldEqual, 1)
			So(math.Abs(total-1) < 1e-9, ShouldBeTrue)
		})
	})
}

func TestGenerate(t *testing.T) {
	Convey("Given an unambiguous continuation", t, func() {
		store := NewStore(WithDecayFactor(1), WithRandomSource(rand.NewSource(3)))
		store.Insert("blue_cab", "Truck")

		Convey("Generate should emit the continuation without the end token", func() {
			sequence := store.Generate("blue_", "Truck", 0, 10)

			So(sequence, ShouldEqual, "cab")
		})
	})
}

func TestBeamSearch(t *testing.T) {
	Convey("Given a populated sequence store", t, func() {
		store := buildSequenceStore()

		Convey("BeamSearch should return ranked completions", func() {
			beams := store.BeamSearch("blue_", "Truck", 3, 1)

			So(len(beams), ShouldEqual, 3)
			So(beams[0].Sequence, ShouldEqual, "cab")
			So(beams[0].Score, ShouldBeGreaterThan, beams[2].Score)
		})
	})
}

func TestExtractPatterns(t *testing.T) {
	Convey("Given repeated sequences in the store", t, func() {
		store := NewStore(WithDecayFactor(1))
		store.Insert("cab", "Truck")
		store.Insert("cab", "Truck")

		Convey("ExtractPatterns should return label-scored repeated symbols", func() {
			patterns := store.ExtractPatterns()

			found := false
			for _, pattern := range patterns {
				if pattern.Symbol == "cab" && pattern.Label == "Truck" {
					found = true
					So(pattern.Score, ShouldBeGreaterThan, 0)
				}
			}

			So(found, ShouldBeTrue)
		})
	})
}

func TestPosteriorsOverTime(t *testing.T) {
	Convey("Given a populated sequence store", t, func() {
		store := buildSequenceStore()

		Convey("PosteriorsOverTime should include the empty context plus each token step", func() {
			posteriors := store.PosteriorsOverTime("blue_cab")

			So(len(posteriors), ShouldEqual, 4)
			So(posteriors[3]["Truck"], ShouldBeGreaterThan, 0)
		})
	})
}

func TestEpisodicBufferSnapshot(t *testing.T) {
	Convey("Given episodic memory enabled", t, func() {
		store := NewStore(
			WithDecayFactor(1),
			WithEpisodicMemory(8),
		)
		store.Insert("alpha_beta", "L")

		Convey("EpisodicBufferSnapshot should expose ids and content tokens", func() {
			snap := store.EpisodicBufferSnapshot()

			So(len(snap), ShouldEqual, 1)
			So(snap[0].Label, ShouldEqual, "L")
			So(snap[0].Tokens, ShouldResemble, []string{"alpha", "beta"})
			So(snap[0].Timestamp, ShouldEqual, 1)
			So(len(snap[0].ID), ShouldBeGreaterThan, 0)
		})
	})
}

func TestReplayOne(t *testing.T) {
	Convey("Given a sequence store without labels", t, func() {
		store := NewStore()

		Convey("ReplayOne should return nil when nothing has been trained", func() {
			So(store.ReplayOne(0.7), ShouldBeNil)
		})
	})
}

func TestTrain(t *testing.T) {
	Convey("Given an empty sequence store", t, func() {
		store := NewStore(WithDecayFactor(1))

		Convey("Train should scale class totals by learningRate", func() {
			store.Train("token", "Label", 0.25)

			So(store.ClassTotals["Label"], ShouldEqual, 0.25)
			So(store.currentStep, ShouldEqual, 1)
		})
	})
}

func TestExperience(t *testing.T) {
	Convey("Given an empty sequence store", t, func() {
		store := NewStore(WithDecayFactor(1), WithRandomSource(rand.NewSource(11)))

		Convey("Experience without label should spawn a concept and train with surprise weighting", func() {
			result := store.Experience("green_alien_spaceship", nil)

			So(result.Label, ShouldEqual, "Concept_1")
			So(result.IsNewConcept, ShouldBeTrue)
			So(result.LearningRate, ShouldBeGreaterThan, 0.5)
			So(store.labels, ShouldContain, "Concept_1")
		})

		Convey("Repeated Experience should reduce learning rate when surprisal falls", func() {
			first := store.Experience("green_alien_spaceship", nil)
			second := store.Experience("green_alien_spaceship", nil)

			So(second.LearningRate, ShouldBeLessThan, first.LearningRate)
			So(second.IsNewConcept, ShouldBeFalse)
		})
	})

	Convey("Given a trained sequence store", t, func() {
		store := buildSequenceStore()

		Convey("Experience should return None for empty semantic content", func() {
			result := store.Experience("   ", nil)

			So(result.Label, ShouldEqual, defaultExperienceEmptyLabel)
			So(result.LearningRate, ShouldEqual, 0)
		})
	})
}

func TestBytePairEncoder_EncodeDocument(t *testing.T) {
	Convey("Given a merge table from a small corpus", t, func() {
		encoder := LearnBytePairEncoder([]string{"abab", "abab cad"}, 2)

		Convey("EncodeDocument should emit subwords separated by underscore delimiters", func() {
			tokens := encoder.EncodeDocument("abab cad")

			So(len(tokens), ShouldBeGreaterThan, 3)
			So(tokens[0], ShouldNotEqual, "a")
		})

		Convey("A sequence store should honor the attached encoder", func() {
			store := NewStore(WithDecayFactor(1), WithBytePairEncoder(encoder))
			store.Insert("abab_cad", "Word")

			So(len(store.Tokenize("abab_cad")) > 4, ShouldBeTrue)
		})
	})
}

func TestSequenceStore_EpisodicMemory(t *testing.T) {
	Convey("Given episodic memory enabled", t, func() {
		store := NewStore(
			WithDecayFactor(1),
			WithEpisodicMemory(32),
			WithEpisodicBlend(0.4),
		)

		store.Insert("alpha_beta_gamma", "Zeta")

		Convey("InterpolatedProbabilities should stay normalized when blending", func() {
			probabilities := store.InterpolatedProbabilities("alpha_", "Zeta")
			total := 0.0

			for _, probability := range probabilities {
				total += probability
			}

			if len(probabilities) > 0 {
				So(math.Abs(total-1) < 1e-6, ShouldBeTrue)
			}
		})
	})
}

func TestMultimodalCoordinator_TrainStep(t *testing.T) {
	Convey("Given a multimodal coordinator", t, func() {
		coordinator := NewMultimodalCoordinator(WithDecayFactor(1))

		Convey("TrainStep should accumulate coactivation mass", func() {
			coordinator.TrainStep("sensory_ctx", "jump", 0.75, "Episode", 1)

			So(
				coordinator.CoactivationStrength("sensory_ctx", "jump", 0.75),
				ShouldEqual,
				1,
			)
		})
	})
}

func TestSequenceStore_DeepestNodeID(t *testing.T) {
	Convey("Given a trained sequence store", t, func() {
		store := NewStore(WithDecayFactor(1))
		store.Insert("a_b", "L")

		Convey("DeepestNodeID should reach the end marker node", func() {
			id := store.DeepestNodeID("a_b")

			So(id, ShouldNotEqual, store.root.ID)
		})
	})
}

func TestPredict(t *testing.T) {
	Convey("Given a populated sequence store", t, func() {
		store := buildSequenceStore()

		Convey("Predict should return classification and continuations", func() {
			prediction := store.Predict("blue_cab")

			So(prediction.Label, ShouldNotBeEmpty)
			So(prediction.Confidence, ShouldBeGreaterThan, 0)
			So(len(prediction.Scores), ShouldEqual, 3)
			So(prediction.Scores["Truck"], ShouldBeGreaterThan, prediction.Scores["Car"])
		})

		Convey("Predict should generate continuations", func() {
			prediction := store.Predict("blue_")

			So(len(prediction.Continuations), ShouldBeGreaterThan, 0)
		})

		Convey("Predict on empty input should return zero value", func() {
			prediction := store.Predict("")

			So(prediction.Label, ShouldBeEmpty)
		})

		Convey("Predict on novel input should learn and classify", func() {
			fresh := NewStore(WithDecayFactor(1), WithRandomSource(rand.NewSource(7)))
			prediction := fresh.Predict("completely_unknown_data")

			So(prediction.Label, ShouldNotBeEmpty)
			So(len(fresh.labels), ShouldBeGreaterThan, 0)
		})
	})
}

func BenchmarkSequenceStore_Insert(b *testing.B) {
	store := NewStore(WithDecayFactor(1))

	b.ResetTimer()

	for b.Loop() {
		store.Insert("blue_cab_big_wheel", "Truck")
	}
}

func BenchmarkSequenceStore_Classify(b *testing.B) {
	store := buildSequenceStore()

	b.ResetTimer()

	for b.Loop() {
		store.Classify("blue_cab")
	}
}

func BenchmarkSequenceStore_BeamSearch(b *testing.B) {
	store := buildSequenceStore()

	b.ResetTimer()

	for b.Loop() {
		store.BeamSearch("blue_", "Truck", 3, 5)
	}
}

func BenchmarkSequenceStore_Experience(b *testing.B) {
	store := NewStore(WithDecayFactor(1), WithRandomSource(rand.NewSource(3)))
	store.Experience("seed_sequence_alpha", nil)

	b.ResetTimer()

	for b.Loop() {
		store.Experience("seed_sequence_alpha", nil)
	}
}
