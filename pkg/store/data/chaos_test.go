package data

import (
	"os"
	"testing"
	"time"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/telemetry"
)

/*
TestChaosRotationDivergence measures how fast two Values diverge
under repeated Rotate3D. 20 seed pairs, full orbit each.
*/
func TestChaosRotationDivergence(t *testing.T) {
	sink := telemetry.NewSink()

	gc.Convey("Given 20 seed pairs under repeated Rotate3D", t, func() {
		for seed := byte(0); seed < 20; seed++ {
			a := BaseValue(seed)
			b := BaseValue(seed + 100)

			valA := a
			valB := b

			initialXOR := valA.XOR(valB).ActiveCount()
			convergences := 0
			maxXOR := 0
			minXOR := 999

			for step := range 128 {
				valA = valA.Rotate3D()
				valB = valB.Rotate3D()

				xorBits := valA.XOR(valB).ActiveCount()

				if xorBits < initialXOR {
					convergences++
				}

				maxXOR = max(maxXOR, xorBits)
				minXOR = min(minXOR, xorBits)

				if step%16 == 0 {
					xorResult := valA.XOR(valB)
					sink.Emit(telemetry.Event{
						Component: "Chaos",
						Action:    "XOR",
						Data: telemetry.EventData{
							ActiveBits: ValuePrimeIndices(&xorResult),
							MatchBits:  ValuePrimeIndices(&valA),
							Density:    float64(xorBits) / 257.0,
							ChunkText:  "Rotate divergence",
						},
					})

					if os.Getenv("TEST_VISUALIZE_TELEMETRY") == "1" {
						if os.Getenv("TEST_VISUALIZE_TELEMETRY") == "1" {
				time.Sleep(50 * time.Millisecond)
			}
					}
				}
			}

			t.Logf("seeds %d,%d: initial XOR=%d, min=%d, max=%d, convergences=%d/128",
				seed, seed+100, initialXOR, minXOR, maxXOR, convergences)

			gc.So(initialXOR, gc.ShouldBeGreaterThan, 0)
		}
	})
}

/*
TestChaosORAccumulation measures how the OR-union grows as we add more Values.
Does it saturate? At what point? 20 values added one by one.
*/
func TestChaosORAccumulation(t *testing.T) {
	sink := telemetry.NewSink()

	gc.Convey("Given 20 Values accumulated via OR", t, func() {
		composite := BaseValue(0)
		prevBits := composite.ActiveCount()

		for i := byte(1); i < 20; i++ {
			member := BaseValue(i * 13)
			composite = composite.OR(member)

			bits := composite.ActiveCount()
			gain := bits - prevBits

			sink.Emit(telemetry.Event{
				Component: "Chaos",
				Action:    "OR",
				Data: telemetry.EventData{
					ActiveBits: ValuePrimeIndices(&composite),
					MatchBits:  ValuePrimeIndices(&member),
					Density:    float64(bits) / 257.0,
					ChunkText:  "OR accumulation",
				},
			})

			if os.Getenv("TEST_VISUALIZE_TELEMETRY") == "1" {
				time.Sleep(80 * time.Millisecond)
			}

			t.Logf("  +BaseValue(%d): total=%d bits, gained=%d", i*13, bits, gain)

			gc.So(bits, gc.ShouldBeGreaterThanOrEqualTo, prevBits)
			prevBits = bits
		}

		t.Logf("Final composite: %d bits active out of 257", prevBits)
	})
}

/*
TestChaosANDErosion measures how AND erodes bits as we intersect more Values.
Opposite of OR accumulation. When does it hit zero?
*/
func TestChaosANDErosion(t *testing.T) {
	sink := telemetry.NewSink()

	gc.Convey("Given 20 Values intersected via AND", t, func() {
		composite := BaseValue(0)
		prevBits := composite.ActiveCount()
		hitZero := -1

		for i := byte(1); i < 20; i++ {
			member := BaseValue(i * 13)
			composite = composite.AND(member)

			bits := composite.ActiveCount()

			sink.Emit(telemetry.Event{
				Component: "Chaos",
				Action:    "AND",
				Data: telemetry.EventData{
					ActiveBits: ValuePrimeIndices(&composite),
					MatchBits:  ValuePrimeIndices(&member),
					Density:    float64(bits) / 257.0,
					ChunkText:  "AND erosion",
				},
			})

			if os.Getenv("TEST_VISUALIZE_TELEMETRY") == "1" {
				time.Sleep(80 * time.Millisecond)
			}

			t.Logf("  AND BaseValue(%d): %d bits remain", i*13, bits)

			gc.So(bits, gc.ShouldBeLessThanOrEqualTo, prevBits)

			if bits == 0 && hitZero == -1 {
				hitZero = int(i)
			}

			prevBits = bits
		}

		t.Logf("AND hit zero at step %d", hitZero)
	})
}

/*
TestChaosXORChain feeds a value through a chain of XOR operations and measures
the distance from origin at each step. Does XOR create random walks? Orbits?
*/
func TestChaosXORChain(t *testing.T) {
	sink := telemetry.NewSink()

	gc.Convey("Given a Value XOR-chained with 20 different values", t, func() {
		origin := BaseValue('A')
		current := origin

		for i := byte(0); i < 20; i++ {
			mask := BaseValue(i * 7)
			current = current.XOR(mask)

			distFromOrigin := origin.XOR(current).ActiveCount()
			simToOrigin := origin.Similarity(current)
			bits := current.ActiveCount()

			sink.Emit(telemetry.Event{
				Component: "Chaos",
				Action:    "XOR",
				Data: telemetry.EventData{
					ActiveBits: ValuePrimeIndices(&current),
					MatchBits:  ValuePrimeIndices(&mask),
					Density:    float64(bits) / 257.0,
					ChunkText:  "XOR chain walk",
				},
			})

			if os.Getenv("TEST_VISUALIZE_TELEMETRY") == "1" {
				time.Sleep(60 * time.Millisecond)
			}

			t.Logf("  XOR step %d (mask=%d): active=%d, dist=%d, sim=%d",
				i, i*7, bits, distFromOrigin, simToOrigin)

			gc.So(bits, gc.ShouldBeGreaterThanOrEqualTo, 0)
		}

		// XOR the entire chain backwards — should return to origin.
		for i := byte(19); i < 20; i-- {
			mask := BaseValue(i * 7)
			current = current.XOR(mask)
		}

		gc.So(current.XOR(origin).ActiveCount(), gc.ShouldEqual, 0)
	})
}

/*
TestChaosHoleCascade cascades Hole operations: start with a dense composite,
progressively strip members out. What remains after each strip?
*/
func TestChaosHoleCascade(t *testing.T) {
	sink := telemetry.NewSink()

	gc.Convey("Given a composite of 10 Values, progressively Hole'd", t, func() {
		members := make([]Value, 10)

		for i := range 10 {
			members[i] = BaseValue(byte(i * 25))
		}

		composite := members[0]

		for _, m := range members[1:] {
			composite = composite.OR(m)
		}

		initialBits := composite.ActiveCount()
		t.Logf("Initial composite: %d bits from 10 members", initialBits)

		sink.Emit(telemetry.Event{
			Component: "Chaos",
			Action:    "Composite",
			Data: telemetry.EventData{
				ActiveBits: ValuePrimeIndices(&composite),
				Density:    float64(initialBits) / 257.0,
				ChunkText:  "Full composite (10 members)",
			},
		})

		if os.Getenv("TEST_VISUALIZE_TELEMETRY") == "1" {
				time.Sleep(100 * time.Millisecond)
			}

		remaining := composite

		for i, m := range members {
			remaining = remaining.Hole(m)
			bits := remaining.ActiveCount()
			memSim := m.Similarity(remaining)

			sink.Emit(telemetry.Event{
				Component: "Chaos",
				Action:    "Hole",
				Data: telemetry.EventData{
					ActiveBits: ValuePrimeIndices(&remaining),
					MatchBits:  ValuePrimeIndices(&m),
					Density:    float64(bits) / 257.0,
					ChunkText:  "Hole cascade strip",
				},
			})

			if os.Getenv("TEST_VISUALIZE_TELEMETRY") == "1" {
				time.Sleep(100 * time.Millisecond)
			}

			t.Logf("  After stripping member %d: %d bits remain, stripped member sim=%d",
				i, bits, memSim)

			gc.So(memSim, gc.ShouldEqual, 0)
		}

		t.Logf("After stripping all 10: %d bits remain", remaining.ActiveCount())
		gc.So(remaining.ActiveCount(), gc.ShouldEqual, 0)
	})
}

/*
TestChaosTriangleInequality checks if Similarity obeys a triangle-like property.
For values A, B, C: does sim(A,C) relate to sim(A,B) + sim(B,C)?
*/
func TestChaosTriangleInequality(t *testing.T) {
	gc.Convey("Given 20 Value triplets, check triangle inequality on Similarity", t, func() {
		violations := 0

		for i := byte(0); i < 20; i++ {
			a := BaseValue(i)
			b := BaseValue(i + 50)
			c := BaseValue(i + 150)

			ab := a.Similarity(b)
			bc := b.Similarity(c)
			ac := a.Similarity(c)

			t.Logf("  triplet %d: sim(A,B)=%d, sim(B,C)=%d, sim(A,C)=%d, sum=%d",
				i, ab, bc, ac, ab+bc)

			if ac > ab+bc {
				violations++
			}
		}

		t.Logf("Triangle inequality violations: %d/20", violations)
		gc.So(violations, gc.ShouldBeLessThanOrEqualTo, 5)
	})
}

/*
TestChaosRotateORInteraction explores what happens when you interleave
Rotate3D and OR operations. Does the density grow? Stay fixed?
*/
func TestChaosRotateORInteraction(t *testing.T) {
	sink := telemetry.NewSink()

	gc.Convey("Given a Value repeatedly rotated then OR'd with its rotation", t, func() {
		seed := BaseValue('X')
		current := seed

		for step := range 20 {
			rotated := current.Rotate3D()
			combined := current.OR(rotated)

			sink.Emit(telemetry.Event{
				Component: "Chaos",
				Action:    "Rotate3D",
				Data: telemetry.EventData{
					ActiveBits: ValuePrimeIndices(&rotated),
					MatchBits:  ValuePrimeIndices(&current),
					Density:    float64(combined.ActiveCount()) / 257.0,
					ChunkText:  "Rotate+OR growth",
				},
			})

			if os.Getenv("TEST_VISUALIZE_TELEMETRY") == "1" {
				time.Sleep(60 * time.Millisecond)
			}

			t.Logf("  step %d: before=%d bits, rotated=%d bits, OR'd=%d bits, overlap=%d",
				step, current.ActiveCount(), rotated.ActiveCount(),
				combined.ActiveCount(), current.Similarity(rotated))

			gc.So(combined.ActiveCount(), gc.ShouldBeGreaterThanOrEqualTo, current.ActiveCount())
			current = combined
		}

		t.Logf("Final density: %d/257 bits", current.ActiveCount())
	})
}

/*
TestChaosRotateANDInteraction same but with AND. Does rotation + AND converge to a fixed point?
*/
func TestChaosRotateANDInteraction(t *testing.T) {
	gc.Convey("Given a Value repeatedly rotated then AND'd with its rotation", t, func() {
		seed := BaseValue('X')
		current := seed

		hitZero := -1

		for step := range 20 {
			rotated := current.Rotate3D()
			combined := current.AND(rotated)

			t.Logf("  step %d: before=%d bits, AND'd=%d bits",
				step, current.ActiveCount(), combined.ActiveCount())

			if combined.ActiveCount() == 0 && hitZero == -1 {
				hitZero = step
			}

			current = combined

			if current.ActiveCount() == 0 {
				break
			}
		}

		t.Logf("AND+Rotate hit zero at step %d", hitZero)
		gc.So(hitZero, gc.ShouldBeGreaterThanOrEqualTo, 0)
		gc.So(current.ActiveCount(), gc.ShouldEqual, 0)
	})
}

/*
TestChaosHoleRouting20Pairs runs 20 pairs through the Hole routing pattern.
Each pair shares a common middle value. Does routing always find the right answer?
*/
func TestChaosHoleRouting20Pairs(t *testing.T) {
	sink := telemetry.NewSink()

	gc.Convey("Given 20 value pairs sharing a common link", t, func() {
		link := BaseValue('L')

		type pair struct {
			left  Value
			right Value
		}

		pairs := make([]pair, 20)

		for i := range 20 {
			pairs[i] = pair{
				left:  BaseValue(byte(i)),
				right: BaseValue(byte(i + 128)),
			}
		}

		branches := make([]Value, 20)
		addrs := make([]Value, 20)

		for i, p := range pairs {
			branch := p.left.OR(link)
			branches[i] = branch.OR(p.right)
			addrs[i] = branches[i].Hole(link)
		}

		correct := 0

		for i, p := range pairs {
			routing := p.left.Hole(link)

			bestIdx := -1
			bestSim := -1

			for j, addr := range addrs {
				sim := routing.Similarity(addr)

				if sim > bestSim {
					bestSim = sim
					bestIdx = j
				}
			}

			if bestIdx == i {
				correct++
			}

			sink.Emit(telemetry.Event{
				Component: "Chaos",
				Action:    "Hole",
				Data: telemetry.EventData{
					ActiveBits: ValuePrimeIndices(&routing),
					MatchBits:  ValuePrimeIndices(&addrs[bestIdx]),
					Density:    float64(bestSim) / float64(max(routing.ActiveCount(), 1)),
					ChunkText:  "Hole routing query",
				},
			})

			if os.Getenv("TEST_VISUALIZE_TELEMETRY") == "1" {
				time.Sleep(40 * time.Millisecond)
			}

			if i < 5 {
				t.Logf("  query %d: routed to %d (expected %d, sim=%d)", i, bestIdx, i, bestSim)
			}
		}

		t.Logf("Routing accuracy: %d/20", correct)
		gc.So(correct, gc.ShouldEqual, 20)
	})
}

/*
TestChaosXORCommutative verifies XOR commutativity across 20 random pairs.
*/
func TestChaosXORCommutative(t *testing.T) {
	gc.Convey("Given 20 Value pairs, XOR is commutative", t, func() {
		for i := byte(0); i < 20; i++ {
			a := BaseValue(i * 3)
			b := BaseValue(i*3 + 50)

			ab := a.XOR(b)
			ba := b.XOR(a)

			gc.So(ab.XOR(ba).ActiveCount(), gc.ShouldEqual, 0)
		}
	})
}

/*
TestChaosXORAssociative verifies XOR associativity across 20 random triples.
*/
func TestChaosXORAssociative(t *testing.T) {
	gc.Convey("Given 20 Value triples, XOR is associative", t, func() {
		for i := byte(0); i < 20; i++ {
			a := BaseValue(i)
			b := BaseValue(i + 80)
			c := BaseValue(i + 160)

			ab_c := a.XOR(b).XOR(c)
			a_bc := a.XOR(b.XOR(c))

			gc.So(ab_c.XOR(a_bc).ActiveCount(), gc.ShouldEqual, 0)
		}
	})
}

/*
TestChaosORSaturationCurve maps the saturation curve: how many distinct
BaseValues must be OR'd before the composite reaches 50%, 75%, 90% of 257 bits?
*/
func TestChaosORSaturationCurve(t *testing.T) {
	sink := telemetry.NewSink()

	gc.Convey("Given all 256 BaseValues OR'd incrementally", t, func() {
		composite := BaseValue(0)

		hit50 := -1
		hit75 := -1
		hit90 := -1
		hit100 := -1

		for i := 1; i < 256; i++ {
			composite = composite.OR(BaseValue(byte(i)))
			pct := composite.ActiveCount() * 100 / 257

			if i%10 == 0 {
				sink.Emit(telemetry.Event{
					Component: "Chaos",
					Action:    "OR",
					Data: telemetry.EventData{
						ActiveBits: ValuePrimeIndices(&composite),
						Density:    float64(composite.ActiveCount()) / 257.0,
						ChunkText:  "Saturation curve",
					},
				})

				if os.Getenv("TEST_VISUALIZE_TELEMETRY") == "1" {
					time.Sleep(30 * time.Millisecond)
				}
			}

			if pct >= 50 && hit50 == -1 {
				hit50 = i
				t.Logf("  50%% at %d values (%d bits)", i, composite.ActiveCount())
			}

			if pct >= 75 && hit75 == -1 {
				hit75 = i
				t.Logf("  75%% at %d values (%d bits)", i, composite.ActiveCount())
			}

			if pct >= 90 && hit90 == -1 {
				hit90 = i
				t.Logf("  90%% at %d values (%d bits)", i, composite.ActiveCount())
			}

			if composite.ActiveCount() == 257 && hit100 == -1 {
				hit100 = i
				t.Logf(" 100%% at %d values (%d bits)", i, composite.ActiveCount())
			}
		}

		t.Logf("Saturation: 50%%@%d 75%%@%d 90%%@%d 100%%@%d", hit50, hit75, hit90, hit100)
		gc.So(hit50, gc.ShouldBeGreaterThan, 0)
	})
}

/*
TestChaosCollisionMap counts how many BaseValue pairs share at least 1 bit.
This is the collision density of the 5-sparse representation.
*/
func TestChaosCollisionMap(t *testing.T) {
	gc.Convey("Given all 256×256 BaseValue pairs, count collisions", t, func() {
		totalPairs := 0
		collisions := 0
		maxSim := 0
		simHist := make(map[int]int)

		for i := 0; i < 256; i++ {
			for j := i + 1; j < 256; j++ {
				a := BaseValue(byte(i))
				b := BaseValue(byte(j))

				sim := a.Similarity(b)
				simHist[sim]++
				totalPairs++

				if sim > 0 {
					collisions++
				}

				maxSim = max(maxSim, sim)
			}
		}

		t.Logf("Total pairs: %d, collisions (sim>0): %d (%.1f%%)",
			totalPairs, collisions, float64(collisions)*100/float64(totalPairs))
		t.Logf("Max similarity between any two BaseValues: %d", maxSim)

		for sim := 0; sim <= 5; sim++ {
			t.Logf("  sim=%d: %d pairs", sim, simHist[sim])
		}

		gc.So(maxSim, gc.ShouldBeLessThan, 5)
	})
}

/*
TestChaosRotateXORInterference rotates two values at different rates
and XORs them together at each step. What pattern emerges?
*/
func TestChaosRotateXORInterference(t *testing.T) {
	sink := telemetry.NewSink()

	gc.Convey("Given two orbits XOR'd at each step (interference pattern)", t, func() {
		a := BaseValue('H')
		b := BaseValue('W')

		for step := range 20 {
			a = a.Rotate3D()

			b = b.Rotate3D()
			b = b.Rotate3D()

			interference := a.XOR(b)

			sink.Emit(telemetry.Event{
				Component: "Chaos",
				Action:    "XOR",
				Data: telemetry.EventData{
					ActiveBits: ValuePrimeIndices(&interference),
					MatchBits:  ValuePrimeIndices(&a),
					Density:    float64(interference.ActiveCount()) / 257.0,
					ChunkText:  "Rotate XOR interference",
				},
			})

			if os.Getenv("TEST_VISUALIZE_TELEMETRY") == "1" {
				time.Sleep(60 * time.Millisecond)
			}

			t.Logf("  step %d: A=%d bits, B=%d bits, A⊕B=%d bits",
				step, a.ActiveCount(), b.ActiveCount(), interference.ActiveCount())

			gc.So(interference.ActiveCount(), gc.ShouldBeGreaterThanOrEqualTo, 0)
		}
	})
}

/*
TestChaosHoleChainRecovery builds A→B→C→D chains via OR, then recovers each
link via cascaded Hole operations. 10 chains of length 4.
*/
func TestChaosHoleChainRecovery(t *testing.T) {
	gc.Convey("Given 10 chains of 4 values, recover links via Hole", t, func() {
		recovered := 0
		total := 0

		for chain := range 10 {
			vals := [4]Value{
				BaseValue(byte(chain * 4)),
				BaseValue(byte(chain*4 + 1)),
				BaseValue(byte(chain*4 + 2)),
				BaseValue(byte(chain*4 + 3)),
			}

			comp := vals[0]

			for _, v := range vals[1:] {
				comp = comp.OR(v)
			}

			remainder := comp.Hole(vals[0])

			for link := 1; link < 4; link++ {
				sim := vals[link].Similarity(remainder)
				total++

				coreCount := vals[link].CoreActiveCount()
				if sim == coreCount {
					recovered++
				}

				if chain < 3 {
					t.Logf("  chain %d, link %d: sim=%d/%d %s",
						chain, link, sim, coreCount,
						func() string {
							if sim == coreCount {
								return "✓"
							}
							return "✗"
						}())
				}
			}
		}

		t.Logf("Recovery rate: %d/%d (%.1f%%)", recovered, total, float64(recovered)*100/float64(total))
		gc.So(recovered, gc.ShouldBeGreaterThanOrEqualTo, total-3)
	})
}

/*
TestChaosValueFingerprint checks if the OR-union of a sequence acts as
a unique fingerprint. 20 sequences of 5 values each — how many collide?
*/
func TestChaosValueFingerprint(t *testing.T) {
	gc.Convey("Given 20 sequences of 5 values, OR-unions should be distinct", t, func() {
		fingerprints := make([]Value, 20)

		for seq := range 20 {
			fp := BaseValue(byte(seq * 5))

			for j := 1; j < 5; j++ {
				fp = fp.OR(BaseValue(byte(seq*5 + j)))
			}

			fingerprints[seq] = fp
		}

		collisions := 0

		for i := range 20 {
			for j := i + 1; j < 20; j++ {
				xor := fingerprints[i].XOR(fingerprints[j])

				if xor.ActiveCount() == 0 {
					collisions++
					t.Logf("  COLLISION: seq %d and %d have identical fingerprints", i, j)
				}
			}
		}

		t.Logf("Fingerprint collisions: %d/190 pairs", collisions)
		gc.So(collisions, gc.ShouldEqual, 0)
	})
}

/*
TestChaosHoleSymmetry checks: is Hole(A, B) related to Hole(B, A)?
They shouldn't be equal (Hole is A & ~B vs B & ~A), but do they complement?
*/
func TestChaosHoleSymmetry(t *testing.T) {
	sink := telemetry.NewSink()

	gc.Convey("Given 20 pairs, Hole(A,B) + Hole(B,A) + AND(A,B) should reconstruct OR(A,B)", t, func() {
		for i := byte(0); i < 20; i++ {
			a := BaseValue(i * 11)
			b := BaseValue(i*11 + 50)

			ab := a.Hole(b)
			ba := b.Hole(a)
			shared := a.AND(b)

			reconstructed := ab.OR(ba)
			reconstructed = reconstructed.OR(shared)
			original := a.OR(b)

			diff := reconstructed.XOR(original).ActiveCount()

			sink.Emit(telemetry.Event{
				Component: "Chaos",
				Action:    "Hole",
				Data: telemetry.EventData{
					ActiveBits: ValuePrimeIndices(&ab),
					MatchBits:  ValuePrimeIndices(&ba),
					CancelBits: ValuePrimeIndices(&shared),
					Density:    float64(shared.ActiveCount()) / 257.0,
					ChunkText:  "Hole symmetry decomposition",
				},
			})

			if os.Getenv("TEST_VISUALIZE_TELEMETRY") == "1" {
				time.Sleep(50 * time.Millisecond)
			}

			if i < 5 {
				t.Logf("  pair %d: A-only=%d, B-only=%d, shared=%d, reconstructed=%d, original=%d, diff=%d",
					i, ab.ActiveCount(), ba.ActiveCount(), shared.ActiveCount(),
					reconstructed.ActiveCount(), original.ActiveCount(), diff)
			}

			gc.So(diff, gc.ShouldEqual, 0)
		}
	})
}

/*
TestChaosRotateGroupAction checks if Rotate3D forms a proper group action:
R(R(A)) should eventually return to A. We check 20 seeds.
*/
func TestChaosRotateGroupAction(t *testing.T) {
	sink := telemetry.NewSink()

	gc.Convey("Given 20 seeds, Rotate3D cycle lengths", t, func() {
		cycleLengths := make(map[int]int)

		for seed := byte(0); seed < 20; seed++ {
			origin := BaseValue(seed * 12)
			current := origin

			cycleLen := 0

			for step := 1; step <= 512; step++ {
				current = current.Rotate3D()
				cycleLen = step

				if step%32 == 0 {
					sink.Emit(telemetry.Event{
						Component: "Chaos",
						Action:    "Rotate3D",
						Data: telemetry.EventData{
							ActiveBits: ValuePrimeIndices(&current),
							Density:    float64(current.ActiveCount()) / 257.0,
							ChunkText:  "Orbit trajectory",
						},
					})

					if os.Getenv("TEST_VISUALIZE_TELEMETRY") == "1" {
					time.Sleep(20 * time.Millisecond)
				}
				}

				if current.XOR(origin).ActiveCount() == 0 {
					break
				}
			}

			cycleLengths[cycleLen]++
			t.Logf("  seed %d: cycle length = %d", seed*12, cycleLen)
		}

		t.Logf("Cycle length distribution:")

		for cl, count := range cycleLengths {
			t.Logf("  length %d: %d seeds", cl, count)
		}

		for seed := byte(0); seed < 20; seed++ {
			origin := BaseValue(seed * 12)
			current := origin

			for range 512 {
				current = current.Rotate3D()
			}

			gc.So(current.XOR(origin).ActiveCount(), gc.ShouldEqual, 0)
		}
	})
}

/*
TestChaosMultiLayerFold stacks 3 operations in sequence (OR, Hole, AND) across
20 value groups. What structure survives all three transformations?
*/
func TestChaosMultiLayerFold(t *testing.T) {
	gc.Convey("Given 20 groups of 3, apply OR→Hole→AND pipeline", t, func() {
		for group := range 20 {
			a := BaseValue(byte(group * 3))
			b := BaseValue(byte(group*3 + 1))
			c := BaseValue(byte(group*3 + 2))

			union := a.OR(b)
			union = union.OR(c)

			bcOnly := union.Hole(a)

			bcShared := b.AND(c)

			finalBits := bcOnly.AND(bcShared)

			if group < 10 {
				t.Logf("  group %d: union=%d, Hole(A)=%d, B∩C=%d, final=%d",
					group, union.ActiveCount(), bcOnly.ActiveCount(),
					bcShared.ActiveCount(), finalBits.ActiveCount())
			}

			gc.So(finalBits.Similarity(bcShared), gc.ShouldEqual, finalBits.ActiveCount())
		}
	})
}

/*
TestChaosValueArithmeticIdentities checks fundamental identities across all 256 byte values.
*/
func TestChaosValueArithmeticIdentities(t *testing.T) {
	gc.Convey("Given all 256 BaseValues, verify algebraic identities", t, func() {
		zero := MustNewValue()

		for b := 0; b < 256; b++ {
			a := BaseValue(byte(b))

			gc.So(a.XOR(zero).XOR(a).ActiveCount(), gc.ShouldEqual, 0)
			gc.So(a.XOR(a).ActiveCount(), gc.ShouldEqual, 0)
			gc.So(a.OR(a).XOR(a).ActiveCount(), gc.ShouldEqual, 0)
			gc.So(a.AND(a).XOR(a).ActiveCount(), gc.ShouldEqual, 0)
			gc.So(a.AND(zero).ActiveCount(), gc.ShouldEqual, 0)
			gc.So(a.Hole(zero).XOR(a).ActiveCount(), gc.ShouldEqual, 0)
			gc.So(zero.Hole(a).ActiveCount(), gc.ShouldEqual, 0)
		}

		t.Logf("All 7 identities hold for all 256 BaseValues (1792 assertions)")
	})
}

/*
TestChaosConvolutionPattern convolves two value sequences: element-wise XOR.
*/
func TestChaosConvolutionPattern(t *testing.T) {
	sink := telemetry.NewSink()

	gc.Convey("Given two 10-value sequences, their XOR convolution", t, func() {
		seqA := make([]Value, 10)
		seqB := make([]Value, 10)

		bytesA := []byte("Hello Worl")
		bytesB := []byte("Hello Moon")

		for i := range 10 {
			seqA[i] = BaseValue(bytesA[i])
			seqB[i] = BaseValue(bytesB[i])
		}

		matchCount := 0
		diffCount := 0

		for i := range 10 {
			xor := seqA[i].XOR(seqB[i])
			bits := xor.ActiveCount()

			match := "≠"
			if bits == 0 {
				match = "="
				matchCount++
			} else {
				diffCount++
			}

			action := "Result"
			if bits > 0 {
				action = "XOR"
			}

			sink.Emit(telemetry.Event{
				Component: "Chaos",
				Action:    action,
				Data: telemetry.EventData{
					ActiveBits: ValuePrimeIndices(&xor),
					MatchBits:  ValuePrimeIndices(&seqA[i]),
					CancelBits: ValuePrimeIndices(&seqB[i]),
					Density:    float64(bits) / 257.0,
					ChunkText:  "Convolution " + match,
				},
			})

			if os.Getenv("TEST_VISUALIZE_TELEMETRY") == "1" {
				time.Sleep(80 * time.Millisecond)
			}

			t.Logf("  pos %d: '%c' %s '%c' → XOR=%d bits",
				i, bytesA[i], match, bytesB[i], bits)
		}

		t.Logf("Matches: %d/10, Diffs: %d/10", matchCount, diffCount)

		// "Hello " + 'o' at position 7 = 7 matches.
		gc.So(matchCount, gc.ShouldEqual, 7)
	})
}
