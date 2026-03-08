package cortex

import (
	"math"
	"reflect"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/numeric"
	"github.com/theapemachine/six/store"
)

// --- Test helpers ---

func setPrimeFieldManifolds(
	t *testing.T,
	field *store.PrimeField,
	manifolds []geometry.IcosahedralManifold,
) {
	t.Helper()

	fieldValue := reflect.ValueOf(field).Elem()

	manifoldsField := fieldValue.FieldByName("manifolds")
	reflect.NewAt(
		manifoldsField.Type(),
		unsafe.Pointer(manifoldsField.UnsafeAddr()),
	).Elem().Set(reflect.ValueOf(manifolds))

	nField := fieldValue.FieldByName("N")
	reflect.NewAt(nField.Type(), unsafe.Pointer(nField.UnsafeAddr())).Elem().SetInt(int64(len(manifolds)))
}

// recordingAnalyzer implements Analyzer for tests that need to observe sequencer calls.
type recordingAnalyzer struct {
	positions []int
	bytes     []byte
}

func (a *recordingAnalyzer) Analyze(pos int, byteVal byte) (bool, []int) {
	a.positions = append(a.positions, pos)
	a.bytes = append(a.bytes, byteVal)
	return false, nil
}

func (a *recordingAnalyzer) Phase() (float64, float64) { return 0, 0 }
func (a *recordingAnalyzer) Phi() float64              { return 0 }

// --- Config & New ---

func TestConfigDefaults(t *testing.T) {
	Convey("Given Config with zero values", t, func() {
		cfg := Config{}

		Convey("When defaults are applied", func() {
			cfg.defaults()

			Convey("It should set InitialNodes to 8", func() {
				So(cfg.InitialNodes, ShouldEqual, 8)
			})
			Convey("It should set MaxTicks to 256", func() {
				So(cfg.MaxTicks, ShouldEqual, 256)
			})
			Convey("It should set MaxOutput to 256", func() {
				So(cfg.MaxOutput, ShouldEqual, 256)
			})
			Convey("It should set ConvergenceWindow to 8", func() {
				So(cfg.ConvergenceWindow, ShouldEqual, 8)
			})
		})
	})
}

func TestNew(t *testing.T) {
	Convey("Given New with InitialNodes 8", t, func() {
		g := New(Config{InitialNodes: 8})

		Convey("It should create a small-world topology", func() {
			So(len(g.Nodes()), ShouldEqual, 8)
			So(g.Source(), ShouldEqual, g.Nodes()[0])
			So(g.Sink(), ShouldEqual, g.Nodes()[7])
		})
		Convey("Source and sink should be distinct", func() {
			So(g.Source(), ShouldNotEqual, g.Sink())
		})
		Convey("Every node should have at least 2 edges (ring + shortcuts)", func() {
			for _, node := range g.Nodes() {
				So(node.EdgeCount(), ShouldBeGreaterThanOrEqualTo, 2)
			}
		})
	})
}

// --- Recall & appendRecallCandidate ---

func TestAppendRecallCandidate(t *testing.T) {
	Convey("Given appendRecallCandidate", t, func() {
		chord := data.BaseChord(42)

		Convey("When adding distinct chords on different faces", func() {
			top := appendRecallCandidate(nil, recallCandidate{chord: chord, face: 10, score: 0.9}, 4)
			top = appendRecallCandidate(top, recallCandidate{chord: chord, face: 11, score: 0.8}, 4)

			Convey("It should preserve both", func() {
				So(len(top), ShouldEqual, 2)
				So(top[0].face, ShouldEqual, 10)
				So(top[1].face, ShouldEqual, 11)
			})
		})
	})
}

func TestRecallScoreFloor(t *testing.T) {
	Convey("Given recallScoreFloor", t, func() {
		Convey("It should be scaled to numeric.ScoreSpace", func() {
			So(recallScoreFloor, ShouldEqual, recallMinFixedScore/numeric.ScoreScale)
		})
		Convey("It should be a sub-0.01 normalized threshold", func() {
			So(recallScoreFloor, ShouldBeLessThan, 0.01)
		})
	})
}

// --- recallQueryManifolds ---

func TestRecallQueryManifolds(t *testing.T) {
	Convey("Given a node with multi-face density", t, func() {
		node := NewNode(0, 0)
		node.Header.SetState(1)
		c20, c21 := data.BaseChord(20), data.BaseChord(21)
		peak := data.ChordOR(&c20, &c21)
		side := data.BaseChord(10)
		other := data.BaseChord(30)
		node.Cube[20] = peak
		node.Cube[10] = side
		node.Cube[30] = other

		anchor, hole, physicalFace, shouldDream := node.Hole()
		So(shouldDream, ShouldBeTrue)

		Convey("When recallQueryManifolds is called with dial 0", func() {
			ctx, expected := recallQueryManifolds(node, anchor, hole, physicalFace, 0, 1, 0)

			Convey("Context should anchor the dominant face", func() {
				So(ctx.Cubes[0][physicalFace], ShouldResemble, anchor)
			})
			Convey("Expected should merge anchor with hole", func() {
				wantExpected := data.ChordOR(&anchor, &hole)
				So(expected.Cubes[0][physicalFace], ShouldResemble, wantExpected)
			})
			Convey("Support cubes should rank by face density", func() {
				So(ctx.Cubes[1][10], ShouldResemble, side)
				So(ctx.Cubes[2][30], ShouldResemble, other)
			})
		})
		Convey("When dial > 0, faces should shift", func() {
			ctx, expected := recallQueryManifolds(node, anchor, hole, physicalFace, 1, 2, math.Pi/2)

			shiftAngle := math.Pi/2 + math.Pi/4
			shift := int(float64(geometry.CubeFaces)*shiftAngle/(2*math.Pi)) % geometry.CubeFaces
			if shift < 0 {
				shift += geometry.CubeFaces
			}
			anchorFace := (physicalFace + shift) % geometry.CubeFaces

			So(ctx.Cubes[0][anchorFace], ShouldResemble, anchor)
			So(expected.Cubes[0][anchorFace], ShouldResemble, data.ChordOR(&anchor, &hole))
		})
	})
}

// --- SpawnNode ---

func TestSpawnNode(t *testing.T) {
	Convey("Given a graph with a parent node having data", t, func() {
		g := New(Config{InitialNodes: 4})
		parent := g.Nodes()[0]
		parent.Cube[10] = data.BaseChord(10)

		Convey("When SpawnNode is called", func() {
			child := g.SpawnNode(parent)

			Convey("Child should be connected to parent", func() {
				edges := child.Edges()
				found := false
				for _, e := range edges {
					if e == parent {
						found = true
						break
					}
				}
				So(found, ShouldBeTrue)
			})
			Convey("Graph should have one more node", func() {
				So(len(g.Nodes()), ShouldEqual, 5)
			})
			Convey("Mitosis events should increment", func() {
				snap := g.Snapshot()
				So(snap.MitosisEvents, ShouldBeGreaterThanOrEqualTo, 1)
			})
		})
	})
}

// --- Routing & Propagation ---

func TestRouteTargets(t *testing.T) {
	Convey("Given a 2-node graph (source directly to sink)", t, func() {
		g := New(Config{InitialNodes: 2})

		Convey("When routeTargets is called from source", func() {
			chord := data.BaseChord('A')
			targets := g.routeTargets(g.Source(), chord)

			Convey("It should return at least one target", func() {
				So(len(targets), ShouldBeGreaterThan, 0)
			})
		})
	})
}

func TestSourceToSink_Propagation(t *testing.T) {
	Convey("Given a 2-node graph", t, func() {
		g := New(Config{InitialNodes: 2})

		sourceHasSink := false
		for _, e := range g.Source().Edges() {
			if e == g.Sink() {
				sourceHasSink = true
				break
			}
		}
		So(sourceHasSink, ShouldBeTrue)

		Convey("When a token is injected and ticks run", func() {
			chord := data.BaseChord('X')
			g.Source().Send(NewDataToken(chord, chord.IntrinsicFace(), -1))
			for range 10 {
				g.Tick()
			}

			Convey("Sink should have positive energy", func() {
				So(g.Sink().Energy(), ShouldBeGreaterThan, 0)
			})
		})
	})
}

func TestSourceToSink_FourNodeGraph(t *testing.T) {
	Convey("Given a 4-node graph", t, func() {
		g := New(Config{InitialNodes: 4})
		chord := data.BaseChord('A')
		g.Source().Send(NewDataToken(chord, chord.IntrinsicFace(), -1))

		for range 100 {
			g.Tick()
		}

		Convey("Sink should receive propagated content", func() {
			So(g.Sink().Energy(), ShouldBeGreaterThan, 0)
		})
	})
}

// --- Bedrock & Recall ---

func TestQueryBedrock_InjectsRecallCandidates(t *testing.T) {
	field := store.NewPrimeField()
	left := data.BaseChord(30)
	right := data.BaseChord(40)
	var manifolds [3]geometry.IcosahedralManifold
	manifolds[1].Cubes[0][30] = left
	manifolds[2].Cubes[0][40] = right
	setPrimeFieldManifolds(t, field, manifolds[:])

	bestFillCalls := 0
	g := New(Config{
		InitialNodes: 4,
		PrimeField:   field,
		BestFill: func(dict unsafe.Pointer, n int, ctx, expected unsafe.Pointer, mode int, geodesic unsafe.Pointer) (int, float64, error) {
			bestFillCalls++
			slice := unsafe.Slice((*geometry.IcosahedralManifold)(dict), n)
			bestIdx, bestScore := -1, 0.0
			for i := range slice {
				if slice[i].Cubes[0][30].ActiveCount() > 0 && 0.9 > bestScore {
					bestIdx, bestScore = i, 0.9
				} else if slice[i].Cubes[0][40].ActiveCount() > 0 && 0.8 > bestScore {
					bestIdx, bestScore = i, 0.8
				}
			}
			if bestIdx < 0 {
				return -1, 0, nil
			}
			return bestIdx, bestScore, nil
		},
	})

	hole := data.ChordOR(&left, &right)
	c5, c6 := data.BaseChord(5), data.BaseChord(6)
	anchor := data.ChordOR(&c5, &c6)
	g.Source().Cube[5] = anchor
	g.Source().Cube[6] = hole

	g.queryBedrock(g.Source())
	recalled := g.Source().DrainInbox()

	Convey("Given queryBedrock with competitive recall", t, func() {
		So(bestFillCalls, ShouldBeGreaterThanOrEqualTo, 1)
		So(len(recalled), ShouldBeGreaterThanOrEqualTo, 1)
	})
}

func TestQueryBedrock_LogicalFaceSpace(t *testing.T) {
	field := store.NewPrimeField()
	recalled := data.BaseChord(30)
	var manifolds [1]geometry.IcosahedralManifold
	manifolds[0].Cubes[0][30] = recalled
	setPrimeFieldManifolds(t, field, manifolds[:])

	g := New(Config{
		InitialNodes: 4,
		PrimeField:   field,
		BestFill: func(dict unsafe.Pointer, n int, _, _ unsafe.Pointer, _ int, _ unsafe.Pointer) (int, float64, error) {
			return 0, 0.9, nil
		},
	})
	g.Source().Rot = geometry.RotationY
	g.Source().Cube[5] = data.BaseChord(5)
	g.Source().Cube[6] = recalled

	g.queryBedrock(g.Source())
	tokens := g.Source().DrainInbox()

	Convey("Given queryBedrock with rotated sink", t, func() {
		So(len(tokens), ShouldEqual, 1)
		wantLogical := g.Source().Rot.Reverse(30)
		So(tokens[0].LogicalFace, ShouldEqual, wantLogical)
	})
}

func TestCollectRecallCandidates_FiltersHoleOverlap(t *testing.T) {
	field := store.NewPrimeField()
	var filler data.Chord
	filler.Set(1)
	var irrelevant data.Chord
	irrelevant.Set(2)
	var manifolds [1]geometry.IcosahedralManifold
	manifolds[0].Cubes[0][30] = filler
	manifolds[0].Cubes[0][40] = irrelevant
	setPrimeFieldManifolds(t, field, manifolds[:])

	g := New(Config{
		PrimeField: field,
		BestFill: func(dict unsafe.Pointer, n int, _, _ unsafe.Pointer, _ int, _ unsafe.Pointer) (int, float64, error) {
			return 0, 0.9, nil
		},
	})
	var anchor data.Chord
	anchor.Set(0)
	hole := filler
	var ctx, expected geometry.IcosahedralManifold
	ptr, n, offset := field.SearchSnapshot()

	candidates := g.collectRecallCandidates(ptr, n, offset, &ctx, &expected, &anchor, &hole, 1)

	Convey("Given collectRecallCandidates", t, func() {
		So(len(candidates), ShouldEqual, 1)
		So(candidates[0].chord, ShouldResemble, filler)
	})
}

// --- Snapshot ---

func TestSnapshot(t *testing.T) {
	Convey("Given a graph after some ticks", t, func() {
		g := New(Config{InitialNodes: 4})
		g.Source().Send(NewDataToken(data.BaseChord('A'), 65, -1))
		for range 5 {
			g.Tick()
		}

		Convey("Snapshot should report totals", func() {
			snap := g.Snapshot()
			So(snap.TotalTicks, ShouldEqual, 5)
			So(snap.FinalNodes, ShouldEqual, 4)
		})
	})
}

func BenchmarkRecallQueryManifolds(b *testing.B) {
	node := NewNode(0, 0)
	for face := 0; face < geometry.CubeFaces; face++ {
		chord := data.BaseChord(byte(face % 256))
		if face%3 == 0 {
			extra := data.BaseChord(byte((face + 1) % 256))
			chord = data.ChordOR(&chord, &extra)
		}
		node.Cube[face] = chord
	}

	anchor, hole, physicalFace, shouldDream := node.Hole()
	if !shouldDream {
		b.Fatal("expected dense node to produce a dream query")
	}

	b.ResetTimer()
	for range b.N {
		_, _ = recallQueryManifolds(node, anchor, hole, physicalFace, 1, 4, math.Pi/3)
	}
}
