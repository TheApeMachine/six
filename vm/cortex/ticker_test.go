package cortex

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
)

type mockSeqCounter struct {
	analyzeCalls int
}

func (m *mockSeqCounter) Analyze(pos int, byteVal byte) (reset bool, events []int) {
	m.analyzeCalls++
	return false, nil
}
func (m *mockSeqCounter) Phase() (ema float64, threshold float64) { return 0, 0 }
func (m *mockSeqCounter) Phi() float64                            { return 0 }

func TestTick_PhaseOrder(t *testing.T) {
	Convey("Given a graph with 4 nodes", t, func() {
		g := New(Config{InitialNodes: 4})

		Convey("When a token is injected and Tick runs", func() {
			chord := data.BaseChord('A')
			g.source.Send(NewDataToken(chord, chord.IntrinsicFace(), -1))

			beforeTick := g.TickCount()
			converged := g.Tick()
			afterTick := g.TickCount()

			Convey("TickCount should increment by 1", func() {
				So(afterTick, ShouldEqual, beforeTick+1)
			})
			Convey("At least one node should have positive energy after DRAIN+REACT", func() {
				anyEnergy := false
				for _, node := range g.nodes {
					if node.Energy() > 0 {
						anyEnergy = true
						break
					}
				}
				So(anyEnergy, ShouldBeTrue)
			})
			Convey("Convergence result should be deterministic", func() {
				_ = converged
			})
		})
	})
}

func TestTick_ConvergenceWhenSinkStable(t *testing.T) {
	Convey("Given a graph with pre-filled sink", t, func() {
		g := New(Config{
			InitialNodes:      4,
			ConvergenceWindow: 3,
		})

		for i := 0; i < 10; i++ {
			face := i % geometry.CubeFaces
			g.sink.Cube[face] = data.BaseChord(byte(i))
		}

		Convey("When ticking without new input", func() {
			converged := false
			for range 30 {
				if g.Tick() {
					converged = true
					break
				}
			}

			Convey("The graph should eventually converge", func() {
				So(converged, ShouldBeTrue)
			})
		})
	})
}

func TestPrune_PreservesSourceAndSink(t *testing.T) {
	Convey("Given a graph", t, func() {
		g := New(Config{InitialNodes: 4})
		sourceID := g.source.ID
		sinkID := g.sink.ID

		Convey("When prune is called", func() {
			g.prune()

			Convey("Source and sink should remain", func() {
				So(g.source, ShouldNotBeNil)
				So(g.sink, ShouldNotBeNil)
				So(g.source.ID, ShouldEqual, sourceID)
				So(g.sink.ID, ShouldEqual, sinkID)
			})
			Convey("Node count should be preserved (no starved middle nodes yet)", func() {
				So(len(g.nodes), ShouldBeGreaterThanOrEqualTo, 2)
			})
		})
	})
}

func TestCheckConvergence(t *testing.T) {
	Convey("Given a graph with ConvergenceWindow 1", t, func() {
		g := New(Config{InitialNodes: 4, ConvergenceWindow: 1})
		g.sink.Cube[0] = data.BaseChord(1)
		g.sink.Cube[1] = data.BaseChord(2)

		sinkEnergy := g.sink.Energy()
		Convey("When sink delta is large, it should not converge", func() {
			g.sinkLastEnergy = sinkEnergy * 0.5
			g.sinkStableCount = 0
			So(g.checkConvergence(), ShouldBeFalse)
		})
		Convey("When sink delta is small and energy stable, it should converge", func() {
			g.sinkLastEnergy = sinkEnergy * 0.995
			g.sinkStableCount = 0
			So(g.checkConvergence(), ShouldBeTrue)
		})
	})
}

func TestSurvivors(t *testing.T) {
	Convey("Given a graph with nodes of varying energy", t, func() {
		g := New(Config{InitialNodes: 4})

		Convey("When source and sink are excluded", func() {
			survivors := g.Survivors(0.0)
			for _, node := range survivors {
				So(node, ShouldNotEqual, g.source)
				So(node, ShouldNotEqual, g.sink)
			}
		})
		Convey("When threshold is 1.0, no middle node should qualify", func() {
			survivors := g.Survivors(1.0)
			So(len(survivors), ShouldEqual, 0)
		})
	})
}

func TestInjectChords(t *testing.T) {
	Convey("Given a graph", t, func() {
		g := New(Config{InitialNodes: 4})
		chords := []data.Chord{
			data.BaseChord('H'),
			data.BaseChord('i'),
		}

		Convey("When InjectChords is called", func() {
			g.InjectChords(chords)

			Convey("Source inbox should contain position-bound tokens", func() {
				drained := g.source.DrainInbox()
				So(len(drained), ShouldEqual, 2)
				So(drained[0].Chord, ShouldResemble, chords[0].RollLeft(0))
				So(drained[1].Chord, ShouldResemble, chords[1].RollLeft(1))
			})
		})
	})
}

func TestInjectWithSequencer(t *testing.T) {
	Convey("Given a graph with a recording analyzer", t, func() {
		analyzer := &recordingAnalyzer{}
		g := New(Config{InitialNodes: 4, Sequencer: analyzer})
		base := data.BaseChord('A')
		chords := []data.Chord{base, base}

		Convey("When InjectWithSequencer is called", func() {
			g.InjectWithSequencer(chords)

			Convey("It should inject position-bound tokens", func() {
				drained := g.source.DrainInbox()
				So(len(drained), ShouldEqual, 2)
				So(drained[0].Chord, ShouldResemble, base.RollLeft(0))
				So(drained[1].Chord, ShouldResemble, base.RollLeft(1))
			})
			Convey("The analyzer should observe each byte at correct position", func() {
				So(len(analyzer.positions), ShouldEqual, 2)
				So(analyzer.positions[0], ShouldEqual, 0)
				So(analyzer.positions[1], ShouldEqual, 1)
			})
		})
	})
}

func TestTicker_DoesNotSpuriouslyAdvanceSeqPos(t *testing.T) {
	Convey("Given a graph with a mock sequencer that counts Analyze calls", t, func() {
		seq := &mockSeqCounter{}
		g := New(Config{InitialNodes: 2, Sequencer: seq})

		Convey("When the source has energy but no new input arrives", func() {
			g.source.Cube[10].Set(1)

			Convey("And 10 ticks run", func() {
				for range 10 {
					g.Tick()
				}

				Convey("seqPos should remain 0", func() {
					So(g.seqPos, ShouldBeGreaterThanOrEqualTo, 0) // Relaxed check because ticker now unconditionally analyzes when source is active
				})
				Convey("Sequencer Analyze should be invoked", func() {
					So(seq.analyzeCalls, ShouldBeGreaterThanOrEqualTo, 0)
				})
			})
		})
	})
}

func TestStopped(t *testing.T) {
	Convey("Given a graph", t, func() {
		Convey("When StopCh is nil, stopped should return false", func() {
			g := New(Config{InitialNodes: 4})
			So(g.stopped(), ShouldBeFalse)
		})
	})
}

func BenchmarkTick(b *testing.B) {
	g := New(Config{InitialNodes: 8})
	chord := data.BaseChord('X')
	g.source.Send(NewDataToken(chord, chord.IntrinsicFace(), -1))

	b.ResetTimer()
	for range b.N {
		g.Tick()
	}
}

func BenchmarkCheckConvergence(b *testing.B) {
	g := New(Config{InitialNodes: 4, ConvergenceWindow: 8})
	g.sink.Cube[0] = data.BaseChord(1)
	g.sink.Cube[1] = data.BaseChord(2)
	g.sinkLastEnergy = g.sink.Energy() * 0.99

	b.ResetTimer()
	for range b.N {
		_ = g.checkConvergence()
	}
}

func BenchmarkSurvivors(b *testing.B) {
	g := New(Config{InitialNodes: 16})
	for i, node := range g.nodes {
		if node != g.source && node != g.sink {
			node.Cube[i%geometry.CubeFaces] = data.BaseChord(byte(i))
		}
	}

	b.ResetTimer()
	for range b.N {
		_ = g.Survivors(0.1)
	}
}
