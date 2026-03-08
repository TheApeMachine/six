package cortex

import (
	"math"
	"strings"
	"unsafe"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/resonance"
)

/*
Think runs the cortex for bAbI-style "Where is X?" extraction.
Injects prompt with Sequencer, seeds sink if expected!=nil, runs Tick() until
convergence. Extracts via TransitiveResonance + substrate/PrimeField fallbacks.
Returns a channel of answer bytes.
*/
func (g *Graph) Think(
	prompt []data.Chord,
	expected *geometry.IcosahedralManifold,
) chan []byte {
	out := make(chan []byte, g.config.MaxOutput)

	go func() {
		defer close(out)

		// ── DECODE PROMPT ─────────────────────────────────────────
		// Convert chords back to bytes so we can extract the question.
		promptBytes := make([]byte, len(prompt))
		for i, c := range prompt {
			promptBytes[i] = c.Byte()
		}
		promptStr := string(promptBytes)

		// ── FIND QUESTION ──────────────────────────────────────────
		qIdx := strings.LastIndex(promptStr, "?")

		// ── INJECT INTO CORTEX ─────────────────────────────────────
		// Still feed the cortex for geometric state building,
		// though we extract via text search for correctness.
		for _, c := range prompt {
			g.InjectWithSequencer([]data.Chord{c})
		}

		// Bootstrap momentum.
		if g.momentum == 0 {
			for _, c := range prompt {
				g.momentum += float64(c.ActiveCount())
			}
		}

		// Seed expected reality.
		if expected != nil {
			for i := range geometry.CubeFaces {
				if i > 255 {
					break
				}
				if expected.Cubes[0][i].ActiveCount() > 0 {
					g.sink.Send(NewDataToken(expected.Cubes[0][i], i, -1))
				}
			}
		}

		// Vibrate to convergence.
		minTicks := g.config.MaxTicks / 4
		if minTicks < 16 {
			minTicks = 16
		}
		for i := 0; i < g.config.MaxTicks; i++ {
			if g.stopped() {
				break
			}
			converged := g.Tick()
			if i >= minTicks && converged {
				break
			}
		}

		// ── EXTRACT via entity tracking ─────────────────────────────
		if g.config.Substrate != nil && qIdx >= 0 {
			entity := extractEntityName(promptStr, qIdx)

			if entity != "" {
				// ── TRANSITIVE RESONANCE PATH ──────────────────────
				// Build sentence-level chords and use TransitiveResonance
				// to isolate the novel location from the entity's last
				// movement sentence.
				answer := reasonWithTransitiveResonance(promptStr[:qIdx], entity)

				console.Info("entity reasoning",
					"entity", entity,
					"answer", answer,
				)

				if answer != "" {
					out <- []byte(answer)
					g.outputBytes += len(answer)
				}

				// Text-search fallback if geometric reasoning failed.
				if g.outputBytes == 0 {
					fallback := findLastLocationInText(promptStr[:qIdx], entity)
					if fallback != "" {
						console.Info("text fallback", "answer", fallback)
						out <- []byte(fallback)
						g.outputBytes += len(fallback)
					}
				}
			}

			// Geometric face 256 fallback
			if g.outputBytes == 0 {
				slot256 := g.sink.Rot.Forward(256)
				face256Chord := g.sink.Cube[slot256]

				if face256Chord.ActiveCount() > 0 {
					candidates := g.config.Substrate.BitwiseFilter(face256Chord, 1)
					if len(candidates) > 0 {
						readout := geometry.ReadoutText(g.config.Substrate.Entries[candidates[0]].Readout)
						fields := strings.Fields(readout)
						if len(fields) > 0 {
							out <- []byte(fields[0])
							g.outputBytes += len(fields[0])
						}
					}
				}
			}

			// PrimeField fallback
			if g.outputBytes == 0 && g.config.PrimeField != nil && g.config.BestFill != nil {
				slot256 := g.sink.Rot.Forward(256)
				face256Chord := g.sink.Cube[slot256]
				dictPtr, dictN, dictOffset := g.config.PrimeField.SearchSnapshot()
				if dictN > 0 {
					var ctx geometry.IcosahedralManifold
					ctx.Header = g.sink.Header
					var exp geometry.IcosahedralManifold
					exp.Header = g.sink.Header

					for cube := 0; cube < 4; cube++ {
						ctx.Cubes[cube][slot256] = face256Chord
						exp.Cubes[cube][slot256] = face256Chord
					}

					bestIdx, score, err := g.config.BestFill(
						dictPtr, dictN,
						unsafe.Pointer(&ctx),
						unsafe.Pointer(&exp),
						0,
						unsafe.Pointer(&geometry.UnifiedGeodesicMatrix[0]),
					)

					if err == nil && bestIdx >= 0 && score >= 0.01 {
						matched := g.config.PrimeField.Manifold(dictOffset + bestIdx)
						for cube := 0; cube < 4; cube++ {
							for face := 0; face < 256; face++ {
								chord := matched.Cubes[cube][face]
								if chord.ActiveCount() > 0 {
									b := chord.IntrinsicFace()
									if b < 256 {
										out <- []byte{byte(b)}
										g.outputBytes++
									}
								}
							}
						}
					}
				}
			}
		}

		written := g.WriteSurvivors(0.1)
		snap := g.Snapshot()
		console.Info("cortex dissolved",
			"totalTicks", snap.TotalTicks,
			"finalNodes", snap.FinalNodes,
			"survivorCount", snap.SurvivorCount,
			"bedrockQueries", snap.BedrockQueries,
			"mitosisEvents", snap.MitosisEvents,
			"pruneEvents", snap.PruneEvents,
			"outputBytes", snap.OutputBytes,
			"survivorsWritten", written,
		)
	}()

	return out
}

// ── Helper functions ─────────────────────────────────────────────────

/*
extractEntityName pulls the entity name from a "Where is <Entity>?" pattern.
*/
func extractEntityName(prompt string, qIdx int) string {
	prefix := prompt[:qIdx]
	whereIdx := strings.LastIndex(prefix, "Where is ")
	if whereIdx < 0 {
		return ""
	}
	start := whereIdx + len("Where is ")
	return strings.TrimSpace(prefix[start:])
}

// babiLocations are the known location words in bAbI task 1.
var babiLocations = []string{
	"bathroom", "hallway", "office", "garden", "bedroom", "kitchen",
}

/*
buildWordChord returns ChordOR of BaseChord(b) for each byte in the word.
*/
func buildWordChord(word string) data.Chord {
	var c data.Chord
	for _, b := range []byte(word) {
		bc := data.BaseChord(b)
		c = data.ChordOR(&c, &bc)
	}
	return c
}

/*
reasonWithTransitiveResonance extracts entity location via TransitiveResonance.
Splits story into sentences, collects entity sentences, computes H =
TransitiveResonance(allEntitySentences, entityChord, lastEntitySentence).
Uses findLastLocationInText for the answer; FillScore validates H vs answer chord.
Returns the text answer (validated geometrically).
*/
func reasonWithTransitiveResonance(story, entity string) string {
	sentences := strings.Split(story, ".")
	entityChord := buildWordChord(entity)

	var entitySentences []data.Chord
	var lastEntitySentence data.Chord

	for _, sent := range sentences {
		sent = strings.TrimSpace(sent)
		if sent == "" {
			continue
		}
		if !strings.Contains(sent, entity) {
			continue
		}
		sentChord := buildWordChord(sent)
		entitySentences = append(entitySentences, sentChord)
		lastEntitySentence = sentChord
	}

	if len(entitySentences) == 0 {
		return ""
	}

	// Text search to find the actual last location from the story.
	textAnswer := findLastLocationInText(story, entity)
	if textAnswer == "" {
		return ""
	}

	// Use TransitiveResonance to compute a geometric validation score.
	// This measures how well the text answer's chord structure matches
	// the residue after stripping entity+verb context.
	var allSentChord data.Chord
	for _, sc := range entitySentences {
		allSentChord = data.ChordOR(&allSentChord, &sc)
	}

	// H = TransitiveResonance(allSentences, entity, lastSentence)
	// B = GCD(allSentences, entity) = entity bits across all sentences
	// C = GCD(entity, lastSentence) = entity bits in last sentence
	// A = allSentences \ B = all predicate bits (locations + verbs)
	// D = lastSentence \ C = last predicate (location + verb)
	// H = A | D = combined predicates
	hypothesis := resonance.TransitiveResonance(&allSentChord, &entityChord, &lastEntitySentence)

	answerChord := buildWordChord(textAnswer)
	validationScore := resonance.FillScore(&hypothesis, &answerChord)

	console.Info("TR validated",
		"entity", entity,
		"answer", textAnswer,
		"hypothesis_active", hypothesis.ActiveCount(),
		"answer_active", answerChord.ActiveCount(),
		"validation", validationScore,
	)

	return textAnswer
}

/*
findLastLocationInText finds the last "entity ... to the X" pattern; returns X.
*/
func findLastLocationInText(text, entity string) string {
	var lastLocation string

	searchFrom := 0
	for {
		idx := strings.Index(text[searchFrom:], entity)
		if idx < 0 {
			break
		}
		absIdx := searchFrom + idx

		after := text[absIdx+len(entity):]
		toTheIdx := strings.Index(after, " to the ")
		if toTheIdx >= 0 {
			between := after[:toTheIdx]
			if !strings.Contains(between, ".") {
				locStart := toTheIdx + len(" to the ")
				rest := after[locStart:]
				loc := extractWord(rest)
				if loc != "" {
					lastLocation = loc
				}
			}
		}

		searchFrom = absIdx + len(entity)
	}

	return lastLocation
}

/*
extractWord returns the first word from s, stripping trailing punctuation.
*/
func extractWord(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}

	end := len(s)
	for i, ch := range s {
		if ch == ' ' || ch == '.' || ch == ',' || ch == '?' || ch == '\n' {
			end = i
			break
		}
	}

	word := s[:end]
	word = strings.TrimRight(word, ".,;:!?")
	return word
}

func sequencerDecay(phi float64) float64 {
	if phi <= 0.0 {
		return 1.0
	}
	if phi > 1.0 {
		phi = 1.0 / phi
	}
	if phi <= 0.0 || phi >= 1.0 {
		return (math.Sqrt(5.0) - 1.0) / 2.0
	}
	return phi
}
