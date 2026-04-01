package task

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/task/classification"
	"github.com/theapemachine/six/experiment/task/codegen"
	"github.com/theapemachine/six/experiment/task/imagegen"
	"github.com/theapemachine/six/experiment/task/logic"
	"github.com/theapemachine/six/experiment/task/misc"
	"github.com/theapemachine/six/experiment/task/phasedial"
	"github.com/theapemachine/six/experiment/task/scaling"
	"github.com/theapemachine/six/experiment/task/textgen"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
)

func TestMain(m *testing.M) {
	viper.SetConfigFile("../../cmd/cfg/config.yml")
	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "failed loading core value config: %v\n", err)
		os.Exit(1)
	}

	// core.Cfg is first filled in core.init() before viper has any keys; rebuild
	// from the loaded file so value regions (e.g. tokens.bits) match config.yml.
	core.NewConfig()

	viper.Set("loglevel", "trace")
	viper.Set("logging.elasticsearch.enabled", true)
	viper.Set("logging.trace.path", os.DevNull)
	viper.Set("logging.elasticsearch.endpoint", "https://127.0.0.1:9200")
	viper.Set("logging.elasticsearch.index", "six-logs")
	viper.Set("logging.elasticsearch.insecure_skip_verify", true)

	loggingCfg, err := core.LoadLoggingConfig()

	if err != nil {
		fmt.Fprintf(os.Stderr, "failed loading logging config: %v\n", err)
		os.Exit(1)
	}

	errnie.InitLogger(loggingCfg)

	// Same wiring as cmd/root: Observer.Emit / EmitFrame go to the global emitter.
	// With `go run . viz --listen`, UDP must match config telemetry.udp_endpoint
	// (default HTTP :8257 → UDP :8258).
	var shutdownTelemetry func()
	if core.Cfg.TelemetryEnabled && strings.TrimSpace(core.Cfg.TelemetryEndpoint) != "" {
		if sender, err := telemetry.NewUDPSender(core.Cfg.TelemetryEndpoint); err == nil {
			telemetry.SetGlobal(sender)
			shutdownTelemetry = func() {
				_ = sender.Close()
				telemetry.SetGlobal(nil)
			}
		} else {
			fmt.Fprintf(os.Stderr, "pipeline tests: telemetry UDP sender disabled: %v\n", err)
		}
	}
	if shutdownTelemetry != nil {
		defer shutdownTelemetry()
	}

	code := m.Run()
	os.Exit(code)
}

// TestPipeline runs each experiment through the real pipeline (hydration, prompts,
// scoring, JSON artifacts). Expectation failures are normal until baselines are met;
// panics and I/O errors are not. A full run takes minutes — use a long -timeout
// (e.g. go test -timeout 30m ./experiment/task -run TestPipeline). The workspace
// .vscode/settings.json sets go.testTimeout to 30m so editor test runs match make;
// without that, IDE defaults (often 30s) abort mid-suite.
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
				// JSON snapshots only — no chromedp/LaTeX/PDF. ProjectorReporter is for
				// paper builds; it can exceed IDE test timeouts when Run awaits Chrome.
				pipeline, err := NewPipeline(
					t.Context(),
					PipelineWithExperiment(experiment),
					PipelineWithReporter(NewSnapshotReporter()),
				)

				So(err, ShouldBeNil)
				So(pipeline, ShouldNotBeNil)

				Convey("When: "+experiment.Name()+" produces an outcome", func() {
					So(pipeline.Run(), ShouldBeNil)

					Convey("It should have the minimum expected outcome for "+experiment.Name(), func() {
						So(experiment.Outcome())
					})

					Convey("It should have produced paper ready artifacts for "+experiment.Name(), func() {
						section := experiment.Section()

						_, resultsErr := os.Stat(
							filepath.Join(
								PaperDir(section),
								tools.Slugify(experiment.Name())+"_results.json",
							),
						)

						So(resultsErr, ShouldBeNil)

						for _, artifact := range experiment.Artifacts() {
							path := filepath.Join(
								PaperDir(section),
								artifactJSONFileName(artifact.FileName),
							)

							_, statErr := os.Stat(path)
							So(statErr, ShouldBeNil)
						}
					})
				})
			})
		})
	}
}

func TestObservePrompt(t *testing.T) {
	Convey("When holdout is empty the partner matches the prompt and learn XOR cancels the token payload", t, func() {
		backend := compute.NewBackgroundBackend()
		defer backend.Close()

		prefix := []byte("orca ")
		value, err := primitive.NewValue(prefix)
		So(err, ShouldBeNil)

		var pipeline Pipeline
		observed, err := pipeline.observePrompt(backend, value, prefix, nil)
		So(err, ShouldBeNil)
		So(len(observed), ShouldEqual, 0)

		value.InstallTombstone()

		var tombPartner primitive.Value
		primitive.CopyFrame(&tombPartner, value)

		So(backend.UniversalBitwise(unsafe.Pointer(value), unsafe.Pointer(&tombPartner)), ShouldBeNil)
		So(value.Close(), ShouldBeNil)
	})

	Convey("When holdout is non-empty the partner encodes the supervised full sample so readout is non-empty", t, func() {
		backend := compute.NewBackgroundBackend()
		defer backend.Close()

		prefix := []byte("orca ")
		holdout := []byte("whale")
		value, err := primitive.NewValue(prefix)
		So(err, ShouldBeNil)

		var pipeline Pipeline
		observed, err := pipeline.observePrompt(backend, value, prefix, holdout)
		So(err, ShouldBeNil)
		So(len(observed), ShouldBeGreaterThan, 0)

		value.InstallTombstone()

		var tombPartner primitive.Value
		primitive.CopyFrame(&tombPartner, value)

		So(backend.UniversalBitwise(unsafe.Pointer(value), unsafe.Pointer(&tombPartner)), ShouldBeNil)
		So(value.Close(), ShouldBeNil)
	})
}

func BenchmarkObservePrompt(b *testing.B) {
	backend := compute.NewBackgroundBackend()
	defer backend.Close()

	prefix := []byte("orca ")
	holdout := []byte("whale")

	b.ResetTimer()

	for b.Loop() {
		value, err := primitive.NewValue(prefix)
		if err != nil {
			b.Fatal(err)
		}

		var pipeline Pipeline

		_, _ = pipeline.observePrompt(backend, value, prefix, holdout)

		value.InstallTombstone()

		var tombPartner primitive.Value
		primitive.CopyFrame(&tombPartner, value)

		_ = backend.UniversalBitwise(unsafe.Pointer(value), unsafe.Pointer(&tombPartner))
		_ = value.Close()
	}
}
