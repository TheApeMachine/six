package task

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/experiment/data/local"
	"github.com/theapemachine/six/experiment/task/classification"
	"github.com/theapemachine/six/experiment/task/codegen"
	"github.com/theapemachine/six/experiment/task/imagegen"
	"github.com/theapemachine/six/experiment/task/logic"
	"github.com/theapemachine/six/experiment/task/misc"
	"github.com/theapemachine/six/experiment/task/phasedial"
	"github.com/theapemachine/six/experiment/task/scaling"
	"github.com/theapemachine/six/experiment/task/textgen"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
	"github.com/theapemachine/six/pkg/vm"
)

type testReporter struct{}

func (testReporter) WriteResults(tools.PipelineExperiment) error { return nil }
func (testReporter) WriteArtifact(tools.PipelineExperiment, tools.Artifact) error {
	return nil
}

type holdoutProbeExperiment struct {
	name     string
	prompts  []string
	holdouts [][]byte
	results  []tools.ExperimentalData
	dataset  data.Provider
}

func newHoldoutProbeExperiment(name string, prompts []string, holdouts [][]byte) *holdoutProbeExperiment {
	return &holdoutProbeExperiment{
		name:     name,
		prompts:  append([]string(nil), prompts...),
		holdouts: append([][]byte(nil), holdouts...),
		results:  []tools.ExperimentalData{},
		dataset:  local.New(local.WithStrings(prompts)),
	}
}

func (experiment *holdoutProbeExperiment) Name() string { return experiment.name }

func (experiment *holdoutProbeExperiment) Section() string { return "test" }

func (experiment *holdoutProbeExperiment) Dataset() data.Provider { return experiment.dataset }

func (experiment *holdoutProbeExperiment) Prompts() []string {
	return append([]string(nil), experiment.prompts...)
}

func (experiment *holdoutProbeExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}

	return append([]byte(nil), experiment.holdouts[idx]...), true
}

func (experiment *holdoutProbeExperiment) AddResult(result tools.ExperimentalData) {
	experiment.results = append(experiment.results, result)
}

func (experiment *holdoutProbeExperiment) Outcome() (any, Assertion, any) {
	return 1.0, ShouldEqual, 1.0
}

func (experiment *holdoutProbeExperiment) TableData() any { return experiment.results }

func (experiment *holdoutProbeExperiment) Artifacts() []tools.Artifact { return nil }

const promptOutputTimeout = 2 * time.Second

type outputBus struct {
	frames chan []byte
}

func newOutputBus() *outputBus {
	return &outputBus{frames: make(chan []byte, 128)}
}

func (bus *outputBus) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

func (bus *outputBus) Write(p []byte) (n int, err error) {
	// Prompt chunks must signal completion even when DecodeTokenIDs yields no
	// bytes (e.g. trailing whitespace-only span or all candidates filtered).
	// Skipping the channel would strand pipeline_test waiting for frame N.
	frame := make([]byte, len(p))
	copy(frame, p)

	bus.frames <- frame

	return len(p), nil
}

func (bus *outputBus) nextFrame(timeout time.Duration) ([]byte, error) {
	select {
	case frame, ok := <-bus.frames:
		if !ok {
			return nil, io.EOF
		}

		return frame, nil
	case <-time.After(timeout):
		return nil, errors.New("timed out waiting for prompt output frame")
	}
}

func frameToValueWords(frame []byte) ([128]uint64, bool) {
	if len(frame) < core.Cfg.Value.Bytes {
		return [128]uint64{}, false
	}

	var words [128]uint64

	for i := range words {
		offset := i * 8
		words[i] = binary.LittleEndian.Uint64(frame[offset:])
	}

	return words, true
}

func frameWord(frame []byte, wordIndex int) uint64 {
	byteOffset := wordIndex * 8
	if byteOffset+8 > len(frame) {
		return 0
	}

	return binary.LittleEndian.Uint64(frame[byteOffset:])
}

func readPromptFrameID(frame []byte) uint64 {
	if len(frame) == 0 {
		return 0
	}

	return frameWord(frame, core.Cfg.Value.Region.ID.Start)
}

func readPromptFrameNextID(frame []byte) uint64 {
	if len(frame) == 0 {
		return 0
	}

	return frameWord(frame, core.Cfg.Value.Region.Next.Start)
}

func collectOutput(bus *outputBus, firstID uint64) (values [][]byte, err error) {
	const deadlineSlack = 50 * time.Millisecond

	if firstID == 0 {
		return nil, errors.New("first prompt frame id must be non-zero")
	}

	deadline := time.Now().Add(promptOutputTimeout)
	pending := map[uint64][]byte{}
	nextID := firstID

	for nextID != 0 {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, errors.New("timed out while collecting linked output frames")
		}

		if frame, ok := pending[nextID]; ok {
			values = append(values, frame)
			delete(pending, nextID)
			nextID = readPromptFrameNextID(frame)
			continue
		}

		frame, readErr := bus.nextFrame(remaining + deadlineSlack)
		if readErr != nil {
			return nil, readErr
		}

		frameID := readPromptFrameID(frame)
		if frameID == 0 {
			continue
		}

		pending[frameID] = frame
	}

	return values, nil
}

func promptFrames(prompt, holdout []byte) ([]*primitive.Value, error) {
	payload := bytes.ReplaceAll(prompt, holdout, []byte{})
	chunkSize := int(vm.TokenizerChunkBytes())
	if chunkSize <= 0 {
		chunkSize = 1
	}

	values := make([]*primitive.Value, 0, max(1, len(payload)/chunkSize+1))

	for offset := 0; offset < len(payload); offset += chunkSize {
		limit := offset + chunkSize

		if limit > len(payload) {
			limit = len(payload)
		}

		value, err := primitive.NewValue(payload[offset:limit])
		if err != nil {
			return nil, err
		}

		if err := value.InstallFirmware(core.FirmwareTypePrompt); err != nil {
			return nil, err
		}

		values = append(values, value)
	}

	if len(values) == 0 {
		empty, err := primitive.NewValue(nil)
		if err != nil {
			return nil, err
		}

		if err := empty.InstallFirmware(core.FirmwareTypePrompt); err != nil {
			return nil, err
		}

		values = append(values, empty)
	}

	for idx := range values {
		var prev, next uint64

		if idx > 0 {
			prev = values[idx-1].GetWord(core.Cfg.Value.Region.ID.Start)
		}

		if idx < len(values)-1 {
			next = values[idx+1].GetWord(core.Cfg.Value.Region.ID.Start)
		}

		values[idx].Link(prev, next)
	}

	return values, nil
}

func TestMain(m *testing.M) {
	viper.SetConfigFile("../../cmd/cfg/config.yml")
	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "failed loading core value config: %v\n", err)
		os.Exit(1)
	}

	// core.Cfg is first filled in core.init() before viper has any keys; rebuild
	// from the loaded file so value regions (e.g. tokens.bits) match config.yml.
	core.NewConfig()

	// Match cmd/root: per-slot UniversalBitwise UDP when telemetry.* flags allow it.
	telemetry.WireUniversalBitwiseSlotHook()

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

/*
evolutionBudget is the time the substrate gets to evolve structure from the
corpus before prompts are injected. This must be long enough for several
batch cycles of signal emission + HolographicCrossover to run.
*/
const evolutionBudget = 2 * time.Second

// TestPipeline runs each experiment through the real pipeline:
//
//  1. Load corpus — all dataset Values are created with Learn firmware and
//     queued for backend execution.
//  2. Evolve — the substrate runs for evolutionBudget. Signal emission creates
//     child Values, HolographicCrossover evolves programs, and the population
//     recirculates through handleFollowUp.
//  3. Prompt — inject prompt Values, wait for settle, walk the output chain.
//  4. Score — compare observed output to holdout, record ExperimentalData.
//  5. Artifacts — write results and artifact JSON/TeX files.
//
// Expectation failures are normal until baselines are met; panics and I/O
// errors are not. A full run takes minutes — use a long -timeout (e.g.
// go test -timeout 30m ./experiment/task -run TestPipeline).
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
				reporter := NewSnapshotReporter()

				pipeline, err := NewPipeline(
					t.Context(),
					PipelineWithExperiment(experiment),
					PipelineWithReporter(reporter),
				)

				So(err, ShouldBeNil)
				So(pipeline, ShouldNotBeNil)

				Convey("And a loaded substrate", func() {
					outputBus := newOutputBus()

					machine, err := vm.NewMachine(
						t.Context(),
						vm.WithOutput(outputBus),
					)

					So(err, ShouldBeNil)
					So(machine, ShouldNotBeNil)
					defer machine.Close()

					// ── Phase 1: Load corpus ──────────────────────────────
					corpusCount, loadErr := machine.LoadCorpus(experiment.Dataset())
					So(loadErr, ShouldBeNil)

					errnie.Info("task.TestPipeline: corpus loaded",
						"experiment", experiment.Name(),
						"values", corpusCount,
					)

					// ── Phase 2: Let evolution run ────────────────────────
					// The backend is processing batches in the background.
					// Signal emission creates child Values, evolution blends
					// programs, and handleFollowUp recirculates. Give it time.
					time.Sleep(evolutionBudget)

					// ── Phase 3: Inject prompts and collect output ────────
					for idx, prompt := range experiment.Prompts() {
						Convey(fmt.Sprintf("When prompted with prompt-%d", idx), func() {
							holdout, ok := experiment.HoldoutForPrompt(idx)
							if !ok {
								holdout = []byte(nil)
							}

							promptValues, promptErr := promptFrames(
								[]byte(prompt),
								holdout,
							)
							So(promptErr, ShouldBeNil)
							So(len(promptValues), ShouldBeGreaterThan, 0)

							// Write prompts through the Machine so they flow
							// through the normal pipeline: tokenizer → backend →
							// waitForPrompt → DecodeTokenIDs → outputBus.
							for _, value := range promptValues {
								_, copyErr := io.Copy(machine, value)
								So(copyErr, ShouldBeNil)
							}

							// Collect output: the prompt's output flows through
							// Machine.Read → outputBus via the normal pipeline.
							// Give each prompt frame time to be processed.
							observedParts := make([][]byte, 0, len(promptValues))
							for range promptValues {
								frame, frameErr := outputBus.nextFrame(promptOutputTimeout)
								if frameErr != nil {
									errnie.Warn("task.TestPipeline: prompt output timeout",
										"experiment", experiment.Name(),
										"prompt_idx", idx,
										"err", frameErr,
									)
									break
								}
								observedParts = append(observedParts, frame)
							}

							observed := bytes.Join(observedParts, nil)

							// ── Phase 4: Score ───────────────────────────
							scores := tools.ByteScores(holdout, observed)
							weighted := tools.WeightedTotalWithWeights(
								pipeline.scoreWgts,
								scores.Exact, scores.Partial, scores.Fuzzy,
							)

							experiment.AddResult(tools.ExperimentalData{
								Idx:           idx,
								Name:          fmt.Sprintf("prompt-%d", idx),
								Prefix:        []byte(prompt),
								Holdout:       holdout,
								Observed:      observed,
								Scores:        scores,
								WeightedTotal: weighted,
							})

							errnie.Trace(
								"task.TestPipeline",
								"prompt", prompt,
								"holdout", string(holdout),
								"observed", string(observed),
								"scores", fmt.Sprintf("%+v", scores),
							)

							Convey("It should have produced output", func() {
								So(observed, ShouldNotBeNil)
							})
						})
					}

					// ── Phase 5: Write artifacts ──────────────────────────
					Convey("It should write results and artifacts for "+experiment.Name(), func() {
						writeErr := reporter.WriteResults(experiment)
						So(writeErr, ShouldBeNil)

						for _, artifact := range experiment.Artifacts() {
							artErr := reporter.WriteArtifact(experiment, artifact)
							So(artErr, ShouldBeNil)
						}

						// Verify the files exist on disk.
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

				Convey("It should have the minimum expected outcome for "+experiment.Name(), func() {
					So(experiment.Outcome())
				})
			})
		})
	}
}
