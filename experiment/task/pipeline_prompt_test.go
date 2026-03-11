package task

import (
	"context"
	"testing"
	"time"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/pool"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/tokenizer"
	"github.com/theapemachine/six/vm"
	"github.com/theapemachine/six/vm/cortex"
)

type PipelinePromptExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *tokenizer.Prompt
}

type PipelineCenterPromptExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *tokenizer.Prompt
}

func NewPipelinePromptExperiment() *PipelinePromptExperiment {
	full := "Mary moved to the bathroom. John went to the hallway. Where is Mary?bathroom"

	return &PipelinePromptExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   NewLocalProvider([]string{full}),
		prompt: tokenizer.NewPrompt(
			tokenizer.PromptWithSamples([]tokenizer.PromptSample{
				{
					Visible: "Mary moved to the bathroom. John went to the hallway. Where is Mary?",
					HeldOut: "bathroom",
					Full:    full,
				},
			}),
		),
	}
}

func NewPipelineCenterPromptExperiment() *PipelineCenterPromptExperiment {
	dataset := NewLocalProvider([]string{
		"abxqr",
		"abypr",
	})

	return &PipelineCenterPromptExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   dataset,
	}
}

func (experiment *PipelinePromptExperiment) Name() string {
	return "Pipeline Prompt"
}

func (experiment *PipelinePromptExperiment) Section() string {
	return "logic"
}

func (experiment *PipelinePromptExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *PipelinePromptExperiment) Prompts() *tokenizer.Prompt {
	return experiment.prompt
}

func (experiment *PipelinePromptExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 0, tokenizer.RIGHT
}

func (experiment *PipelinePromptExperiment) AddResult(result tools.ExperimentalData) {
	result.Scores = tools.ByteScores(result.Holdout, result.Observed)
	result.WeightedTotal = tools.WeightedTotal(
		result.Scores.Exact,
		result.Scores.Partial,
		result.Scores.Fuzzy,
	)

	experiment.tableData = append(experiment.tableData, result)
}

func (experiment *PipelinePromptExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldEqual, 1.0
}

func (experiment *PipelinePromptExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *PipelinePromptExperiment) Artifacts() []tools.Artifact {
	return nil
}

func (experiment *PipelinePromptExperiment) Finalize(*geometry.HybridSubstrate) error {
	return nil
}

func (experiment *PipelinePromptExperiment) RawOutput() bool { return false }

func (experiment *PipelinePromptExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}

	return experiment.tableData[0].WeightedTotal
}

func (experiment *PipelineCenterPromptExperiment) Name() string {
	return "Pipeline Prompt Center"
}

func (experiment *PipelineCenterPromptExperiment) Section() string {
	return "logic"
}

func (experiment *PipelineCenterPromptExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *PipelineCenterPromptExperiment) Prompts() *tokenizer.Prompt {
	experiment.prompt = tokenizer.NewPrompt(
		tokenizer.PromptWithDataset(experiment.dataset),
		tokenizer.PromptWithHoldout(experiment.Holdout()),
	)

	return experiment.prompt
}

func (experiment *PipelineCenterPromptExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 20, tokenizer.CENTER
}

func (experiment *PipelineCenterPromptExperiment) AddResult(result tools.ExperimentalData) {
	result.Scores = tools.ByteScores(result.Holdout, result.Observed)
	result.WeightedTotal = tools.WeightedTotal(
		result.Scores.Exact,
		result.Scores.Partial,
		result.Scores.Fuzzy,
	)

	experiment.tableData = append(experiment.tableData, result)
}

func (experiment *PipelineCenterPromptExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldEqual, 1.0
}

func (experiment *PipelineCenterPromptExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *PipelineCenterPromptExperiment) Artifacts() []tools.Artifact {
	return nil
}

func (experiment *PipelineCenterPromptExperiment) Finalize(*geometry.HybridSubstrate) error {
	return nil
}

func (experiment *PipelineCenterPromptExperiment) RawOutput() bool { return false }

func (experiment *PipelineCenterPromptExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}

	total := 0.0
	for _, row := range experiment.tableData {
		total += row.WeightedTotal
	}

	return total / float64(len(experiment.tableData))
}

func pipelineTextToChords(text string) []data.Chord {
	out := make([]data.Chord, 0, len(text))
	for i := range len(text) {
		out = append(out, data.BaseChord(text[i]))
	}

	return out
}

func pipelineChordsToText(chords []data.Chord) string {
	buf := make([]byte, 0, len(chords))
	for _, chord := range chords {
		buf = append(buf, chord.BestByte())
	}

	return string(buf)
}

func TestPipelinePromptUsesPromptContextReadout(t *testing.T) {
	gc.Convey("Given a pipeline experiment with an explicit visible prefix and held-out answer", t, func() {
		experiment := NewPipelinePromptExperiment()
		pipeline, err := NewPipeline(
			PipelineWithExperiment(experiment),
			PipelineWithReporter(NewSnapshotReporter()),
		)

		gc.So(err, gc.ShouldBeNil)
		gc.So(pipeline, gc.ShouldNotBeNil)

		gc.Convey("When the pipeline runs", func() {
			err = pipeline.Run()

			gc.So(err, gc.ShouldBeNil)
			gc.So(experiment.tableData, gc.ShouldHaveLength, 1)
			gc.So(string(experiment.tableData[0].Observed), gc.ShouldEqual, "bathroom")
			gc.So(experiment.tableData[0].Scores.Exact, gc.ShouldEqual, 1.0)
		})
	})
}

func TestPipelinePromptUsesBoundaryConditionsForCenterMask(t *testing.T) {
	gc.Convey("Given a center-held prompt whose left boundary is ambiguous", t, func() {
		experiment := NewPipelineCenterPromptExperiment()
		pipeline, err := NewPipeline(
			PipelineWithExperiment(experiment),
			PipelineWithReporter(NewSnapshotReporter()),
		)

		gc.So(err, gc.ShouldBeNil)
		gc.So(pipeline, gc.ShouldNotBeNil)

		gc.Convey("When the pipeline runs", func() {
			err = pipeline.Run()

			gc.So(err, gc.ShouldBeNil)
			gc.So(experiment.tableData, gc.ShouldHaveLength, 2)
			gc.So(string(experiment.tableData[0].Observed), gc.ShouldEqual, "x")
			gc.So(string(experiment.tableData[1].Observed), gc.ShouldEqual, "y")
			gc.So(experiment.Score(), gc.ShouldEqual, 1.0)
		})
	})
}

func TestPipelineSolvePromptReadoutUsesLogicCircuits(t *testing.T) {
	gc.Convey("Given a prompt solve where corpus bias conflicts with a cortex circuit", t, func() {
		loader := vm.NewLoader(
			vm.LoaderWithTokenizer(
				tokenizer.NewUniversal(
					tokenizer.TokenizerWithDataset(NewLocalProvider([]string{
						"axxxr",
						"axxxr",
						"axxxr",
						"abcer",
						"abcer",
					})),
				),
			),
		)

		gc.So(loader.Start(), gc.ShouldBeNil)

		pipeline := &Pipeline{
			loader: loader,
			composer: vm.NewBoundaryComposer(
				loader,
				vm.BoundaryComposerWithTopK(12),
				vm.BoundaryComposerWithDomainLimit(10),
			),
			prompts: tokenizer.NewPrompt(
				tokenizer.PromptWithDataset(NewLocalProvider([]string{"axxxr"})),
				tokenizer.PromptWithHoldout(60, tokenizer.CENTER),
			),
		}

		p1 := data.BaseChord('!')
		p2 := data.BaseChord('?')
		p3 := data.BaseChord('+')
		program := data.ChordOR(&p1, &p2)
		program = data.ChordOR(&program, &p3)

		logic := cortex.LogicSnapshot{Circuits: []cortex.LogicCircuit{{
			Steps: []cortex.LogicRule{
				{Interface: data.BaseChord('a'), Payload: data.BaseChord('c'), Program: p1, Support: 16, Role: cortex.RoleTool},
				{Interface: data.BaseChord('c'), Payload: data.BaseChord('d'), Program: p2, Support: 15, Role: cortex.RoleTool},
				{Interface: data.BaseChord('d'), Payload: data.BaseChord('e'), Program: p3, Support: 14, Role: cortex.RoleTool},
			},
			Program: program,
			Support: 12,
		}}}

		gc.Convey("When solvePromptReadout composes the masked span", func() {
			readout := pipeline.solvePromptReadout(
				pipelineTextToChords("a"),
				pipelineTextToChords("r"),
				"xxx",
				nil,
				logic,
			)

			gc.Convey("It should thread logic circuits into the composer and override the local corpus bias", func() {
				gc.So(pipelineChordsToText(readout), gc.ShouldEqual, "cde")
			})
		})
	})
}

func TestPipelineShouldUsePromptLogicCircuits(t *testing.T) {
	gc.Convey("Given prompt-time cortex circuits", t, func() {
		pipeline := &Pipeline{}
		logic := cortex.LogicSnapshot{Circuits: []cortex.LogicCircuit{{
			Steps: []cortex.LogicRule{{
				Interface: data.BaseChord('a'),
				Payload:   data.BaseChord('b'),
				Support:   4,
				Role:      cortex.RoleTool,
			}},
			Support: 4,
		}}}

		leftVisible := pipelineTextToChords("a")
		rightVisible := pipelineTextToChords("r")

		gc.Convey("It should allow short bounded spans to use circuit guidance", func() {
			gc.So(pipeline.shouldUsePromptLogicCircuits(3, "xyz", leftVisible, rightVisible, logic), gc.ShouldBeTrue)
		})

		gc.Convey("It should reject long or whitespace-heavy spans that trigger synthetic boundaries", func() {
			gc.So(pipeline.shouldUsePromptLogicCircuits(32, "func main() {\n\treturn 1\n}", leftVisible, rightVisible, logic), gc.ShouldBeFalse)
		})

		gc.Convey("It should reject large one-sided prompt masks like right-held code completions", func() {
			gc.So(pipeline.shouldUsePromptLogicCircuits(266, "return longGeneratedSuffix", leftVisible, nil, logic), gc.ShouldBeFalse)
		})
	})
}

func TestPipelineCollectPromptOutputsIgnoresStalePromptCycles(t *testing.T) {
	gc.Convey("Given prompt-scoped result messages on the broadcast", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		workerPool := pool.New(ctx, 1, 1, nil)
		broadcast := workerPool.CreateBroadcastGroup("prompt-collector", time.Second)
		pipeline := &Pipeline{broadcast: broadcast}

		systemCh := broadcast.Subscribe("prompt-collector-system", 10)
		defer broadcast.Unsubscribe("prompt-collector-system")

		expected := pipelineTextToChords("fresh")
		promptID := uint64(7)

		go func() {
			for res := range systemCh {
				pv, ok := res.Value.(pool.PoolValue[cortex.PromptCycle])
				if !ok || pv.Key != "prompt" || pv.Value.ID != promptID {
					continue
				}

				broadcast.Send(pool.NewResult(*pool.NewPoolValue(
					pool.WithKey[cortex.PromptLogic]("logic"),
					pool.WithValue(cortex.PromptLogic{
						PromptID: promptID - 1,
						Snapshot: cortex.LogicSnapshot{Signals: pipelineTextToChords("stale")},
					}),
				)))

				broadcast.Send(pool.NewResult(*pool.NewPoolValue(
					pool.WithKey[cortex.PromptResult]("results"),
					pool.WithValue(cortex.PromptResult{
						PromptID: promptID - 1,
						Chords:   pipelineTextToChords("stale"),
					}),
				)))

				broadcast.Send(pool.NewResult(*pool.NewPoolValue(
					pool.WithKey[cortex.PromptLogic]("logic"),
					pool.WithValue(cortex.PromptLogic{
						PromptID: promptID,
						Snapshot: cortex.LogicSnapshot{Signals: expected},
					}),
				)))

				broadcast.Send(pool.NewResult(*pool.NewPoolValue(
					pool.WithKey[cortex.PromptResult]("results"),
					pool.WithValue(cortex.PromptResult{
						PromptID: promptID,
						Chords:   expected,
					}),
				)))

				return
			}
		}()

		chordRes, logic := pipeline.collectPromptOutputs(pipelineTextToChords("prompt"), promptID)

		gc.So(pipelineChordsToText(chordRes), gc.ShouldEqual, "fresh")
		gc.So(pipelineChordsToText(logic.Signals), gc.ShouldEqual, "fresh")
	})
}
