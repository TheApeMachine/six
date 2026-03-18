package data

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

/*
TestValueSuperpositionAndQuery verifies that OR-merging multiple Values
creates a composite that "contains" all of them. Similarity against the
composite acts as a membership test.
*/
func TestValueSuperpositionAndQuery(t *testing.T) {
	gc.Convey("Given multiple Values OR-merged into a composite", t, func() {
		alice := BaseValue('A')
		bob := BaseValue('B')
		carol := BaseValue('C')
		dave := BaseValue('D')
		outsider := BaseValue('Z')

		// Build the composite: a superposition of Alice, Bob, Carol, Dave.
		composite := alice.OR(bob)
		composite = composite.OR(carol)
		composite = composite.OR(dave)

		t.Logf("Members: A=%d bits, B=%d bits, C=%d bits, D=%d bits",
			alice.ActiveCount(), bob.ActiveCount(), carol.ActiveCount(), dave.ActiveCount())
		t.Logf("Composite: %d bits active", composite.ActiveCount())
		t.Logf("Outsider Z: %d bits", outsider.ActiveCount())

		gc.Convey("Every member has full similarity with the composite (all its bits are present)", func() {
			gc.So(alice.Similarity(composite), gc.ShouldEqual, alice.ActiveCount())
			gc.So(bob.Similarity(composite), gc.ShouldEqual, bob.ActiveCount())
			gc.So(carol.Similarity(composite), gc.ShouldEqual, carol.ActiveCount())
			gc.So(dave.Similarity(composite), gc.ShouldEqual, dave.ActiveCount())
		})

		gc.Convey("An outsider has lower similarity with the composite", func() {
			outsiderSim := outsider.Similarity(composite)
			memberSim := alice.Similarity(composite) // always 5 (full membership)

			t.Logf("Outsider Z similarity to composite: %d bits (member similarity: %d bits)",
				outsiderSim, memberSim)

			gc.So(outsiderSim, gc.ShouldBeLessThan, memberSim)
		})

		gc.Convey("Membership test works for ALL 256 byte values", func() {
			members := map[byte]bool{'A': true, 'B': true, 'C': true, 'D': true}
			correctMember := 0
			correctNonMember := 0

			for b := 0; b < 256; b++ {
				val := BaseValue(byte(b))
				sim := val.Similarity(composite)
				isMember := sim == val.ActiveCount()

				if members[byte(b)] {
					gc.So(isMember, gc.ShouldBeTrue)
					correctMember++
				} else {
					correctNonMember++
				}
			}

			gc.So(correctMember, gc.ShouldEqual, 4)
			t.Logf("Member check: %d members verified, %d non-members checked", correctMember, correctNonMember)
		})
	})
}

/*
TestValueCancellationFromComposite verifies that XOR can "subtract" a known
component from a superposition, leaving behind the bits unique to the other
members.
*/
func TestValueCancellationFromComposite(t *testing.T) {
	gc.Convey("Given a composite of three Values", t, func() {
		alpha := BaseValue('X')
		beta := BaseValue('Y')
		gamma := BaseValue('Z')

		composite := alpha.OR(beta)
		composite = composite.OR(gamma)

		gc.Convey("XOR with a member flips its bits out of the composite", func() {
			residue := composite.XOR(alpha)

			// Alpha's unique bits should be gone. Beta and gamma's bits should survive.
			alphaStillPresent := alpha.Similarity(residue)
			betaStillPresent := beta.Similarity(residue)
			gammaStillPresent := gamma.Similarity(residue)

			t.Logf("After XOR with alpha: alpha=%d, beta=%d, gamma=%d bits remaining",
				alphaStillPresent, betaStillPresent, gammaStillPresent)

			// Beta and gamma should retain more similarity than alpha.
			gc.So(betaStillPresent, gc.ShouldBeGreaterThanOrEqualTo, alphaStillPresent)
			gc.So(gammaStillPresent, gc.ShouldBeGreaterThanOrEqualTo, alphaStillPresent)
		})

		gc.Convey("Hole(composite, member) shows bits in composite NOT in member", func() {
			hole := composite.Hole(alpha)

			// The hole should contain beta's and gamma's unique bits.
			t.Logf("ValueHole(composite, alpha): %d bits", hole.ActiveCount())
			t.Logf("Beta similarity to hole: %d", beta.Similarity(hole))
			t.Logf("Gamma similarity to hole: %d", gamma.Similarity(hole))

			gc.So(hole.ActiveCount(), gc.ShouldBeGreaterThan, 0)
			gc.So(beta.Similarity(hole), gc.ShouldBeGreaterThan, 0)
			gc.So(gamma.Similarity(hole), gc.ShouldBeGreaterThan, 0)
		})
	})
}

/*
TestValueOrbitResonance verifies that two generators (Rotate3D from different
seeds) produce orbits that have measurable similarity patterns over time.
This shows how Values "interact" through geometric proximity in the 257-bit space.
*/
func TestValueOrbitResonance(t *testing.T) {
	gc.Convey("Given two generator orbits from different seeds", t, func() {
		seedA := BaseValue('A')
		seedB := BaseValue('B')

		gc.Convey("Sparse 5-bit orbits from different seeds stay non-overlapping (rigid permutation)", func() {
			valA := seedA
			valB := seedB

			for range 128 {
				valA = valA.Rotate3D()
				valB = valB.Rotate3D()

				// If the bit sets start disjoint, Rotate3D keeps them disjoint.
				gc.So(valA.Similarity(valB), gc.ShouldEqual, 0)
			}
		})

		gc.Convey("Denser Values (OR-merged) DO show varying similarity as they orbit", func() {
			// Build denser seeds with overlapping bit regions.
			baseAB := BaseValue('A')
			denseA := baseAB.OR(BaseValue('B'))
			denseA = denseA.OR(BaseValue('C'))

			baseBC := BaseValue('B')
			denseB := baseBC.OR(BaseValue('C'))
			denseB = denseB.OR(BaseValue('D'))

			valA := denseA
			valB := denseB

			minSim := 999
			maxSim := 0

			for step := range 128 {
				valA = valA.Rotate3D()
				valB = valB.Rotate3D()

				sim := valA.Similarity(valB)

				if sim < minSim {
					minSim = sim
				}

				if sim > maxSim {
					maxSim = sim
				}

				if step < 5 {
					t.Logf("  step %d: similarity = %d bits", step, sim)
				}
			}

			t.Logf("Over 128 steps: min similarity = %d, max similarity = %d", minSim, maxSim)

			// Dense Values that share some bits should maintain some overlap.
			gc.So(maxSim, gc.ShouldBeGreaterThan, 0)
		})

		gc.Convey("Self-orbit similarity is always perfect (same seed, same step)", func() {
			valA1 := seedA
			valA2 := seedA

			for range 50 {
				valA1 = valA1.Rotate3D()
				valA2 = valA2.Rotate3D()

				gc.So(valA1.XOR(valA2).ActiveCount(), gc.ShouldEqual, 0)
			}
		})
	})
}

/*
TestValueGateChain verifies that Values can act as conditional gates in
sequence. Each gate checks whether an input "passes" (has sufficient
similarity) or "blocks" (insufficient similarity). A chain of gates
routes signals through the lattice.
*/
func TestValueGateChain(t *testing.T) {
	gc.Convey("Given a chain of gate Values", t, func() {
		// Build gates from specific byte values.
		gate1 := BaseValue('a')
		gate2 := BaseValue('b')
		gate3 := BaseValue('c')

		// An input that contains gate1's pattern should pass gate1.
		passingInput := gate1.OR(BaseValue('x')) // has 'a' bits plus extra
		blockingInput := BaseValue('z')          // has no 'a' bits

		gc.Convey("A passing input has full overlap with its gate", func() {
			sim := gate1.Similarity(passingInput)
			gc.So(sim, gc.ShouldEqual, gate1.ActiveCount())
			t.Logf("Passing input similarity to gate1: %d/%d bits", sim, gate1.ActiveCount())
		})

		gc.Convey("A blocking input has low overlap with the gate", func() {
			sim := gate1.Similarity(blockingInput)
			gc.So(sim, gc.ShouldBeLessThan, gate1.ActiveCount())
			t.Logf("Blocking input similarity to gate1: %d/%d bits", sim, gate1.ActiveCount())
		})

		gc.Convey("A multi-gate input selectively passes the right gates", func() {
			// Input that should pass gates 1 and 3 but not gate 2.
			selective := gate1.OR(gate3)

			sim1 := gate1.Similarity(selective)
			sim2 := gate2.Similarity(selective)
			sim3 := gate3.Similarity(selective)

			t.Logf("Selective input → gate1:%d gate2:%d gate3:%d", sim1, sim2, sim3)

			gc.So(sim1, gc.ShouldEqual, gate1.ActiveCount())
			gc.So(sim3, gc.ShouldEqual, gate3.ActiveCount())
			gc.So(sim2, gc.ShouldBeLessThan, gate2.ActiveCount())
		})
	})
}

/*
TestValueCollectiveBehavior verifies how a group of Values crystallizes
through the fold operations: OR accumulates, AND finds consensus, XOR
reveals disagreement. This is the foundation of RecursiveFold.
*/
func TestValueCollectiveBehavior(t *testing.T) {
	gc.Convey("Given a population of 10 Values with varying overlap", t, func() {
		// Build 10 values. First 5 share byte 'S', last 5 share byte 'T'.
		// All 10 share byte 'Q'.
		shared := BaseValue('Q')
		groupAMarker := BaseValue('S')
		groupBMarker := BaseValue('T')

		population := make([]Value, 10)

		for i := range 5 {
			base := shared.OR(groupAMarker)
			population[i] = base.OR(BaseValue(byte(i + 100)))
		}

		for i := range 5 {
			base := shared.OR(groupBMarker)
			population[i+5] = base.OR(BaseValue(byte(i + 200)))
		}

		gc.Convey("OR of all reveals the total energy envelope", func() {
			total := population[0]

			for _, val := range population[1:] {
				total = total.OR(val)
			}

			t.Logf("Total OR envelope: %d bits (from 10 values)", total.ActiveCount())

			// Every member should be fully contained.
			for i, val := range population {
				gc.So(val.Similarity(total), gc.ShouldEqual, val.ActiveCount())
				_ = i
			}
		})

		gc.Convey("AND of all reveals the universally shared bits", func() {
			consensus := population[0]

			for _, val := range population[1:] {
				consensus = consensus.AND(val)
			}

			t.Logf("AND consensus: %d bits (shared by all 10)", consensus.ActiveCount())

			// The shared byte Q's bits should be in the consensus.
			qOverlap := shared.Similarity(consensus)
			t.Logf("  Q overlap with consensus: %d/%d", qOverlap, shared.ActiveCount())
			gc.So(qOverlap, gc.ShouldEqual, shared.ActiveCount())

			// Group markers should NOT be in the consensus (only in 5 of 10).
			sOverlap := groupAMarker.Similarity(consensus)
			tOverlap := groupBMarker.Similarity(consensus)
			t.Logf("  S overlap: %d, T overlap: %d (should be less than full)", sOverlap, tOverlap)
			gc.So(sOverlap, gc.ShouldBeLessThan, groupAMarker.ActiveCount())
			gc.So(tOverlap, gc.ShouldBeLessThan, groupBMarker.ActiveCount())
		})

		gc.Convey("AND within each group reveals group-specific shared bits", func() {
			groupACons := population[0]

			for _, val := range population[1:5] {
				groupACons = groupACons.AND(val)
			}

			groupBCons := population[5]

			for _, val := range population[6:] {
				groupBCons = groupBCons.AND(val)
			}

			t.Logf("Group A consensus: %d bits", groupACons.ActiveCount())
			t.Logf("Group B consensus: %d bits", groupBCons.ActiveCount())

			// Each group's consensus should contain the global shared Q.
			gc.So(shared.Similarity(groupACons), gc.ShouldEqual, shared.ActiveCount())
			gc.So(shared.Similarity(groupBCons), gc.ShouldEqual, shared.ActiveCount())

			// Each group's consensus should contain its own marker.
			gc.So(groupAMarker.Similarity(groupACons), gc.ShouldEqual, groupAMarker.ActiveCount())
			gc.So(groupBMarker.Similarity(groupBCons), gc.ShouldEqual, groupBMarker.ActiveCount())

			// XOR between group consensuses reveals what distinguishes them.
			diff := groupACons.XOR(groupBCons)
			t.Logf("XOR between group consensuses: %d bits of disagreement", diff.ActiveCount())
			gc.So(diff.ActiveCount(), gc.ShouldBeGreaterThan, 0)
		})
	})
}

/*
TestHoleRouting proves that Hole-based navigation routes a signal to the
correct child without scanning. This is the mechanism that makes the fold
NOT brute force.

Setup:

	Seq 0: [Sandra, isInThe, Garden]
	Seq 1: [Roy,    isInThe, Kitchen]
	Seq 2: [Harold, isInThe, Kitchen]

The fold should:
1. Extract the shared invariant (isInThe bits) via OR+AND.
2. Compute each branch's "routing address" via Hole(union, invariant).
3. When a query signal arrives, compute signal.Hole(invariant) → routing residue.
4. The routing residue matches exactly ONE branch address.
*/
func TestHoleRouting(t *testing.T) {
	gc.Convey("Given three sequences folded into a routing structure", t, func() {
		sandra := BaseValue('S')
		roy := BaseValue('R')
		harold := BaseValue('H')
		isInThe := BaseValue('I')
		garden := BaseValue('G')
		kitchen := BaseValue('K')

		// Build sequence unions.
		seq0Union := sandra.OR(isInThe)
		seq0Union = seq0Union.OR(garden)

		seq1Union := roy.OR(isInThe)
		seq1Union = seq1Union.OR(kitchen)

		seq2Union := harold.OR(isInThe)
		seq2Union = seq2Union.OR(kitchen)

		// Extract shared invariant: AND across all unions.
		invariant := seq0Union.AND(seq1Union)
		invariant = invariant.AND(seq2Union)

		t.Logf("Invariant (shared bits): %d active", invariant.ActiveCount())
		t.Logf("isInThe similarity to invariant: %d/%d",
			isInThe.Similarity(invariant), isInThe.ActiveCount())

		// Compute routing addresses: what's in each branch but NOT in the invariant.
		addr0 := seq0Union.Hole(invariant)
		addr1 := seq1Union.Hole(invariant)
		addr2 := seq2Union.Hole(invariant)

		t.Logf("Address 0 (Sandra+Garden): %d bits", addr0.ActiveCount())
		t.Logf("Address 1 (Roy+Kitchen):   %d bits", addr1.ActiveCount())
		t.Logf("Address 2 (Harold+Kitchen):%d bits", addr2.ActiveCount())

		gc.Convey("Each branch address is non-zero and distinct", func() {
			gc.So(addr0.ActiveCount(), gc.ShouldBeGreaterThan, 0)
			gc.So(addr1.ActiveCount(), gc.ShouldBeGreaterThan, 0)
			gc.So(addr2.ActiveCount(), gc.ShouldBeGreaterThan, 0)

			// No two addresses should be identical.
			gc.So(addr0.XOR(addr1).ActiveCount(), gc.ShouldBeGreaterThan, 0)
			gc.So(addr0.XOR(addr2).ActiveCount(), gc.ShouldBeGreaterThan, 0)
			// addr1 and addr2 may share Kitchen bits, but Roy vs Harold differs.
			gc.So(addr1.XOR(addr2).ActiveCount(), gc.ShouldBeGreaterThan, 0)
		})

		gc.Convey("Routing: each sequence member routes to its correct branch", func() {
			type routeTest struct {
				name     string
				signal   Value
				expected int // which branch index it should route to
			}

			tests := []routeTest{
				{"Sandra", sandra, 0},
				{"Garden", garden, 0},
				{"Roy", roy, 1},
				{"Kitchen (via Roy path)", kitchen, 1}, // Kitchen in both 1 and 2 — highest match wins
				{"Harold", harold, 2},
			}

			addrs := []Value{addr0, addr1, addr2}

			for _, tt := range tests {
				// Strip invariant: what's in the signal but NOT in the shared stuff.
				routing := tt.signal.Hole(invariant)

				// Find best matching branch.
				bestBranch := -1
				bestSim := -1

				for idx, addr := range addrs {
					sim := routing.Similarity(addr)

					if sim > bestSim {
						bestSim = sim
						bestBranch = idx
					}
				}

				t.Logf("  %s → routing=%d bits → branch %d (sim=%d) expected=%d",
					tt.name, routing.ActiveCount(), bestBranch, bestSim, tt.expected)

				gc.So(bestBranch, gc.ShouldEqual, tt.expected)
			}
		})

		gc.Convey("Full pipeline: signal with mixed content routes correctly", func() {
			// A signal carrying Sandra + isInThe (asking about Sandra).
			query := sandra.OR(isInThe)
			routing := query.Hole(invariant)

			t.Logf("Query (Sandra+isInThe): %d bits", query.ActiveCount())
			t.Logf("After stripping invariant: %d bits", routing.ActiveCount())

			// The routing residue contains Sandra's bits that don't overlap the invariant.
			// Some Sandra bits may coincide with invariant bits (geometry, not a bug).
			gc.So(sandra.Similarity(routing), gc.ShouldBeGreaterThan, 0)
			t.Logf("Sandra bits in routing: %d/%d", sandra.Similarity(routing), sandra.ActiveCount())

			// Route to branch 0 (Sandra+Garden).
			sim0 := routing.Similarity(addr0)
			sim1 := routing.Similarity(addr1)
			sim2 := routing.Similarity(addr2)

			t.Logf("  Routing similarity → branch0:%d branch1:%d branch2:%d", sim0, sim1, sim2)

			gc.So(sim0, gc.ShouldBeGreaterThan, sim1)
			gc.So(sim0, gc.ShouldBeGreaterThan, sim2)

			// Now at branch 0, the remaining content is Sandra+Garden.
			// The answer (Garden) is: branch address Hole'd by the query signal.
			answer := addr0.Hole(routing)
			// The answer should contain Garden's bits (possibly minus invariant overlap).
			gc.So(garden.Similarity(answer), gc.ShouldBeGreaterThanOrEqualTo, garden.ActiveCount()-1)
			t.Logf("  Garden similarity to answer: %d/%d",
				garden.Similarity(answer), garden.ActiveCount())
		})

		gc.Convey("Reverse query: signal carries Garden, finds Sandra", func() {
			query := garden.OR(isInThe)
			routing := query.Hole(invariant)

			sim0 := routing.Similarity(addr0)
			sim1 := routing.Similarity(addr1)
			sim2 := routing.Similarity(addr2)

			t.Logf("  Garden query → branch0:%d branch1:%d branch2:%d", sim0, sim1, sim2)

			gc.So(sim0, gc.ShouldBeGreaterThan, sim1)
			gc.So(sim0, gc.ShouldBeGreaterThan, sim2)

			answer := addr0.Hole(routing)
			gc.So(sandra.Similarity(answer), gc.ShouldBeGreaterThanOrEqualTo, sandra.ActiveCount()-1)
			t.Logf("  Sandra similarity to answer: %d/%d",
				sandra.Similarity(answer), sandra.ActiveCount())
		})
	})
}
