package task

import (
	"fmt"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/task/classification"
	"github.com/theapemachine/six/experiment/task/codegen"
	"github.com/theapemachine/six/experiment/task/imagegen"
	"github.com/theapemachine/six/experiment/task/logic"
	"github.com/theapemachine/six/experiment/task/misc"
	"github.com/theapemachine/six/experiment/task/phasedial"
	"github.com/theapemachine/six/experiment/task/scaling"
	"github.com/theapemachine/six/experiment/task/textgen"
	"github.com/theapemachine/six/pkg/vm"
)

/*
TestPipeline runs the experiments for the research paper.
It autoamtically builds the paper artifacts so experiment
results are always an accurate representation of current
architecture capabilities.

 1. Load corpus — all dataset Values are created with Learn firmware and
    queued for backend execution.
 2. Evolve — the substrate runs for evolutionBudget. Signal emission creates
    child Values, HolographicCrossover evolves programs, and the population
    recirculates through handleFollowUp.
 3. Prompt — inject prompt Values, wait for settle, walk the output chain.
 4. Score — compare observed output to holdout, record ExperimentalData.
 5. Artifacts — write results and artifact JSON/TeX files.

Expectation failures are normal until baselines are met; panics and I/O
errors are not. A full run takes minutes — use a long -timeout (e.g.
go test -timeout 30m ./experiment/task -run TestPipeline).
*/
func TestPipeline(t *testing.T) {
	allExperiments := []tools.PipelineExperiment{
		codegen.NewLanguagesExperiment(),
		classification.NewTextClassificationExperiment(),
		textgen.NewCompositionalExperiment(),
		textgen.NewProseChainingExperiment(),
		textgen.NewOutOfCorpusExperiment(),
		textgen.NewTextOverlapExperiment(),
		phasedial.NewAdaptiveSplitExperiment(),
		phasedial.NewChunkingBaselineExperiment(),
		phasedial.NewConstraintResolutionExperiment(),
		phasedial.NewCorrelationLengthExperiment(),
		phasedial.NewGroupActionEquivarianceExperiment(),
		phasedial.NewPartialDeletionExperiment(),
		phasedial.NewPermutationInvarianceExperiment(),
		phasedial.NewPhaseCoherenceExperiment(),
		phasedial.NewQueryRobustnessExperiment(),
		phasedial.NewSnapToSurfaceExperiment(),
		phasedial.NewSteerabilityExperiment(),
		phasedial.NewTwoHopRetrievalExperiment(),
		imagegen.NewReconstructionExperiment(),
		logic.NewBabiExperiment(),
		logic.NewSemanticAlgebraExperiment(),
		misc.NewCrossDomainCompletionExperiment(),
		misc.NewGemmaIntegrationExperiment(),
		misc.NewRuleShiftExperiment(),
		scaling.NewBestFillScalingExperiment(),
		scaling.NewCompressionExperiment(),
		scaling.NewPipelineThroughputExperiment(),
		scaling.NewSequencerExperiment(),
	}

	for _, experiment := range allExperiments {
		t.Run(experiment.Name(), func(t *testing.T) {
			Convey("Given experiment: "+experiment.Name(), t, func() {
				pipeline, err := NewPipeline(
					t.Context(),
					PipelineWithExperiment(experiment),
					PipelineWithReporter(NewProjectorReporter()),
					PipelineWithSnapshotReporter(),
				)

				So(err, ShouldBeNil)
				So(pipeline, ShouldNotBeNil)

				Convey("And a machine", func() {
					machine, err := vm.NewMachine(
						t.Context(),
						vm.MachineWithDataset(experiment.Dataset()),
					)

					So(err, ShouldBeNil)
					So(machine, ShouldNotBeNil)

					Convey("When the machine is run", func() {
						So(machine.Run(), ShouldBeNil)
					})

					for idx, prompt := range experiment.Prompts() {
						Convey("When prompted with '"+prompt+"'", func() {
							So(machine.Prompt(prompt), ShouldBeNil)

							Convey(
								fmt.Sprintf("It should have the right answer for %s", prompt),
								func() {
									holdout, ok := pipeline.experiment.HoldoutForPrompt(idx)

									So(
										ok,
										ShouldBeTrue,
										fmt.Sprintf("expected holdout for prompt index %d (%q)", idx, prompt),
									)
									if !ok {
										return
									}

									So(
										machine.Kadabra().Store.Classify(prompt),
										ShouldEqual,
										string(holdout),
									)
								},
							)
						})
					}
				})

				Convey("It should have the minimum expected outcome for "+experiment.Name(), func() {
					So(experiment.Outcome())
				})
			})
		})
	}
}
