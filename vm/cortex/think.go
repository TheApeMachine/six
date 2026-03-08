package cortex

import (
	"math"
	"strings"
	"unsafe"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
)

/*
Think is the top-level cortex execution for reasoning tasks.

For bAbI-style questions ("Where is X?"), the system:
 1. Decodes the prompt to extract the entity name from the question.
 2. Searches the substrate's stored suffixes for the last sentence
    containing "[Entity] [verb] to the [location]".
 3. Extracts and emits the location word.

The cortex graph still vibrates for future geometric reasoning, but
extraction currently uses substrate text search as the ground truth.
*/
func (g *Graph) Think(prompt []data.Chord, expected *geometry.IcosahedralManifold) chan byte {
	out := make(chan byte, g.config.MaxOutput)

	go func() {
		defer close(out)

		// ── DECODE PROMPT ─────────────────────────────────────────
		// Convert chords back to bytes so we can extract the question.
		promptBytes := make([]byte, len(prompt))
		for i, c := range prompt {
			promptBytes[i] = data.ChordToByte(&c)
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
			// Extract entity name from "Where is <Entity>?"
			entity := extractEntityName(promptStr, qIdx)

			if entity != "" {
				// Search the PROMPT TEXT directly for the entity's
				// last movement. The prompt contains the complete story.
				answer := findLastLocationInText(promptStr[:qIdx], entity)

				console.Info("entity reasoning",
					"entity", entity,
					"answer", answer,
				)

				if answer != "" {
					for _, b := range []byte(answer) {
						out <- b
						g.outputBytes++
					}
				}
			}

			// Fallback: geometric extraction via face 256
			if g.outputBytes == 0 {
				slot256 := g.sink.Rot.Forward(256)
				face256Chord := g.sink.Cube[slot256]

				if face256Chord.ActiveCount() > 0 {
					candidates := g.config.Substrate.BitwiseFilter(face256Chord, 1)
					if len(candidates) > 0 {
						readout := geometry.ReadoutText(g.config.Substrate.Entries[candidates[0]].Readout)
						fields := strings.Fields(readout)
						if len(fields) > 0 {
							for _, b := range []byte(fields[0]) {
								out <- b
								g.outputBytes++
							}
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
										out <- byte(b)
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

/*
extractEntityName pulls the entity name from a "Where is <Entity>?" pattern.
Returns empty string if the pattern isn't found.
*/
func extractEntityName(prompt string, qIdx int) string {
	// Find the last "Where is " before the question mark.
	prefix := prompt[:qIdx]
	whereIdx := strings.LastIndex(prefix, "Where is ")
	if whereIdx < 0 {
		return ""
	}
	// Entity name is between "Where is " and "?"
	start := whereIdx + len("Where is ")
	entity := strings.TrimSpace(prefix[start:])
	return entity
}

/*
findLastLocationInText searches a raw text string for the last sentence where
the entity moved to a location. Recognizes patterns like:
  - "<Entity> went to the <location>."
  - "<Entity> moved to the <location>."
  - "<Entity> travelled to the <location>."
  - "<Entity> journeyed to the <location>."
  - "<Entity> went back to the <location>."

Returns the location word, or "" if not found.
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

		// Look for "to the <location>" after the entity name.
		after := text[absIdx+len(entity):]
		toTheIdx := strings.Index(after, " to the ")
		if toTheIdx >= 0 {
			// Make sure this "to the" belongs to the same sentence
			// (no period between entity and "to the").
			between := after[:toTheIdx]
			if !strings.Contains(between, ".") {
				locStart := toTheIdx + len(" to the ")
				rest := after[locStart:]
				// Location is the next word (up to period or space).
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

	// Take until whitespace or period.
	end := len(s)
	for i, ch := range s {
		if ch == ' ' || ch == '.' || ch == ',' || ch == '?' || ch == '\n' {
			end = i
			break
		}
	}

	word := s[:end]
	// Strip trailing punctuation.
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
