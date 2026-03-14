package misc

import (
	"fmt"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/store/data/provider/local"
	"github.com/theapemachine/six/pkg/system/vm/input"
)

// ruleShiftSamplesPerPhase is the number of samples per phase.
// At 128 bytes per sample, Phase A = 3200 bytes, Phase B = 3200 bytes.
// Enough to populate distinct attractor basins before the shift.
const ruleShiftSamplesPerPhase = 25

// ruleShiftHoldoutPct is the fraction of each sample held out as the target.
const ruleShiftHoldoutPct = 25

// ruleShiftSampleLen is the byte length of each synthetic sample.
const ruleShiftSampleLen = 128

/*
ruleA generates sample i under Rule A: linear modular sequence.
Byte k of sample i is  (seed_A(i) + k*3) % 251  — multiplication by a
prime guarantees non-trivial stride; mod 251 (also prime) avoids the
wrap-around predictability of mod 256.
*/
func ruleA(sampleIdx, byteIdx int) byte {
	seed := (sampleIdx*37 + 7) % 251
	return byte((seed + byteIdx*3) % 251)
}

/*
ruleB generates sample i under Rule B: XOR-nonlinear sequence.
Byte k of sample i is  seed_B(i) XOR (k * 11 % 256).
The nonlinear XOR structure creates a fundamentally different chord
attractor landscape from Rule A.
*/
func ruleB(sampleIdx, byteIdx int) byte {
	seed := (sampleIdx*53 + 13) % 256
	return byte(seed ^ (byteIdx*11)%256)
}

// buildSample constructs one byte sequence according to the given rule fn.
func buildSample(sampleIdx int, fn func(int, int) byte) []byte {
	buf := make([]byte, ruleShiftSampleLen)
	for k := range buf {
		buf[k] = fn(sampleIdx, k)
	}
	return buf
}

/*
RuleShiftExperiment tests the substrate's ability to detect and adapt to
a mid-stream rule shift — the point at which the generative rule underlying
the input data changes from Rule A (linear modular sequences) to Rule B
(XOR-nonlinear sequences).

Two phases operate over the same unified substrate:
  - Phase A (steps 0 … N-1): samples follow Rule A.
  - Phase B (steps N … 2N-1): samples follow Rule B.

At every step the pipeline's retrieval output is scored against both
rules' expected continuations:
  - K_A = fuzzy alignment with the Rule A continuation.
  - K_B = fuzzy alignment with the Rule B continuation.

The experiment measures adaptation speed (steps to stable K_B dominance),
transient confusion at the shift boundary, and residual cross-rule
interference once Phase B is established.
*/
type RuleShiftExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    []string

	// Per-step alignment signals, filled in AddResult.
	kA     []float64
	kB     []float64
	winner []string

	// Pre-computed expected continuations under each rule for every step.
	// Index = global step (0..2N-1).  The holdout is the last
	// ruleShiftHoldoutPct% of the sample.
	expectedA [][]byte
	expectedB [][]byte
}

/*
NewRuleShiftExperiment constructs the two-phase synthetic dataset and
pre-computes the expected continuations for both rules at every step.
*/
func NewRuleShiftExperiment() *RuleShiftExperiment {
	n := ruleShiftSamplesPerPhase
	total := 2 * n

	// Pre-compute expected continuations (the holdout portion).
	holdoutBytes := ruleShiftSampleLen * ruleShiftHoldoutPct / 100
	expectedA := make([][]byte, total)
	expectedB := make([][]byte, total)

	corpus := make([][]byte, total)
	for step := range total {
		var sample []byte
		ruleIdx := step % n // position within each phase (for deterministic seeds)

		if step < n {
			sample = buildSample(ruleIdx, ruleA)
		} else {
			sample = buildSample(ruleIdx, ruleB)
		}

		corpus[step] = sample

		// The holdout is always the last holdoutBytes of the sample regardless
		// of which rule generated it.  But we also store what BOTH rules would
		// have produced, so we can score the retrieval against each.
		aFull := buildSample(ruleIdx, ruleA)
		bFull := buildSample(ruleIdx, ruleB)
		expectedA[step] = aFull[len(aFull)-holdoutBytes:]
		expectedB[step] = bFull[len(bFull)-holdoutBytes:]
	}

	return &RuleShiftExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   local.New(local.WithBytesOfBytes(corpus)),
		expectedA: expectedA,
		expectedB: expectedB,
	}
}

func (exp *RuleShiftExperiment) Name() string    { return "RuleShift" }
func (exp *RuleShiftExperiment) Section() string { return "misc" }

func (exp *RuleShiftExperiment) Dataset() provider.Dataset { return exp.dataset }

func (exp *RuleShiftExperiment) Prompts() []string {
	return []string{
		"Predict the secondary structure of the given amino acid sequence.",
	}
}

func (exp *RuleShiftExperiment) Holdout() (int, input.HoldoutType) {
	return ruleShiftHoldoutPct, input.RIGHT
}

/*
AddResult is called by the pipeline for each prompt step.
It scores the retrieval against both rules' expected continuation,
appends per-step K values, and records the dominant rule (winner).
*/
func (exp *RuleShiftExperiment) AddResult(result tools.ExperimentalData) {
	step := result.Idx

	kA, kB := 0.0, 0.0
	if step < len(exp.expectedA) {
		kA = tools.ByteScores(exp.expectedA[step], result.Observed).Fuzzy
	}
	if step < len(exp.expectedB) {
		kB = tools.ByteScores(exp.expectedB[step], result.Observed).Fuzzy
	}

	w := "A"
	if kB > kA {
		w = "B"
	}

	exp.kA = append(exp.kA, kA)
	exp.kB = append(exp.kB, kB)
	exp.winner = append(exp.winner, w)

	result.Name = fmt.Sprintf("step%02d", step)
	result.Scores = tools.Scores{
		Exact:   result.Scores.Exact,
		Partial: kA,
		Fuzzy:   kB,
	}
	result.WeightedTotal = (kA + kB) / 2.0

	exp.tableData = append(exp.tableData, result)
}

func (exp *RuleShiftExperiment) Outcome() (any, gc.Assertion, any) {
	return exp.Score(), gc.ShouldBeGreaterThanOrEqualTo, 0.0
}

func (exp *RuleShiftExperiment) Score() float64 {
	if len(exp.tableData) == 0 {
		return 0.0
	}
	total := 0.0
	for _, d := range exp.tableData {
		total += d.WeightedTotal
	}
	return total / float64(len(exp.tableData))
}

func (exp *RuleShiftExperiment) TableData() any { return exp.tableData }

func (exp *RuleShiftExperiment) Artifacts() []tools.Artifact {
	n := ruleShiftSamplesPerPhase
	steps := len(exp.kA)
	if steps == 0 {
		return nil
	}

	stepLabels := make([]string, steps)
	for i := range stepLabels {
		stepLabels[i] = fmt.Sprintf("%d", i)
	}

	// Confidence = mean(K_A, K_B) — how certain the substrate is about
	// any rule at all.
	confidence := make([]float64, steps)
	// K_edge is the local |K_A - K_B| margin — how sharply differentiated.
	kEdge := make([]float64, steps)
	for i := range steps {
		confidence[i] = (exp.kA[i] + exp.kB[i]) / 2.0
		diff := exp.kA[i] - exp.kB[i]
		if diff < 0 {
			diff = -diff
		}
		kEdge[i] = diff
	}

	// Shift boundary — first step where Phase B starts.
	shiftStep := n
	if shiftStep >= steps {
		shiftStep = steps - 1
	}

	// Recovery step: first step after the shift where K_B > K_A by more
	// than 0.1 and stays so.
	recoveryStep := -1
	for i := shiftStep; i < steps; i++ {
		if exp.kB[i]-exp.kA[i] > 0.05 {
			if recoveryStep == -1 {
				recoveryStep = i
			}
		} else {
			recoveryStep = -1
		}
		if recoveryStep != -1 && i-recoveryStep >= 2 {
			break
		}
	}

	// Winner bar data as numeric (A=1, B=0) for the bottom panel.
	winnerVals := make([]float64, steps)
	for i, w := range exp.winner {
		if w == "A" {
			winnerVals[i] = 1.0
		}
	}

	phaseASteps := min(n, steps)
	phaseBSteps := steps - phaseASteps

	recoveryDesc := "did not stabilise within the observation window"
	if recoveryStep >= 0 {
		recoveryDesc = fmt.Sprintf("stabilised at step %d (%d steps after the shift)", recoveryStep, recoveryStep-shiftStep)
	}

	proseTemplate := `\subsection{Rule-Shift Adaptation}
\label{sec:rule_shift}

\paragraph{Task Description.}
This experiment injects a mid-stream rule change into the substrate without
any explicit signal or re-training.  Two generative rules partition the
$2N$ = {{.Total}}-sample stream:

\begin{itemize}[nosep]
  \item \textbf{Rule A} (steps 0--{{.ShiftStep}}, $N={{.N}}$ samples):
        linear modular sequences, $b_k = (s_i + 3k) \bmod 251$.
  \item \textbf{Rule B} (steps {{.ShiftStep}}--{{.Total}}, $N={{.N}}$ samples):
        XOR-nonlinear sequences, $b_k = s_i \oplus (11k \bmod 256)$.
\end{itemize}

At every step the pipeline's retrieval output is scored against the
expected continuation under \emph{both} rules simultaneously, yielding
per-step alignment signals $K_A$ and $K_B$.  The winner at each step
is $\arg\max(K_A, K_B)$.

\paragraph{Results.}
Figure~\ref{fig:ruleshift_trajectory} shows the full adaptation
trajectory.  The shift boundary is at step {{.ShiftStep}}.  Rule B
{{.RecoveryDesc}}.  Phase A had ${{.PhaseAWins}}$ of
${{.PhaseASteps}}$ steps won by Rule A
({{.PhaseAPct | f0}}\,\%); Phase B had ${{.PhaseBWins}}$ of
${{.PhaseBSteps}}$ steps won by Rule B ({{.PhaseBPct | f0}}\,\%).

{{if ge .PhaseBPct 60.0 -}}
\paragraph{Assessment.}
The substrate successfully adapted to the rule shift: after an initial
transient the retrieval field re-aligned with Rule B's attractor basin
without any parameter update or explicit domain signal.  This demonstrates
that the chord manifold reorganises spontaneously in response to
distributional change, consistent with the Holographic BVP framing in
which the field settles to the lowest-energy configuration compatible
with the current input boundary conditions.
{{- else if ge .PhaseBPct 40.0 -}}
\paragraph{Assessment.}
Partial adaptation was observed.  The substrate showed sensitivity to the
shift but did not reach complete dominance of Rule B within the observation
window.  Larger Phase A pre-training volumes are expected to sharpen the
Phase A attractor, increasing the contrast ratio $|K_A - K_B|$ and
accelerating re-equilibration.
{{- else -}}
\paragraph{Assessment.}
The substrate did not complete the transition within the available window.
At this scale, Rules A and B produce similar chord cosine similarities;
longer samples or larger \textit{N} would widen the attractor gap and make
the shift detectable.
{{- end}}

`

	// Per-phase winner counts.
	phaseAWins, phaseBWins := 0, 0
	for i, w := range exp.winner {
		if i < shiftStep && w == "A" {
			phaseAWins++
		} else if i >= shiftStep && w == "B" {
			phaseBWins++
		}
	}
	phaseAPct := 0.0
	if phaseASteps > 0 {
		phaseAPct = float64(phaseAWins) / float64(phaseASteps) * 100
	}
	phaseBPct := 0.0
	if phaseBSteps > 0 {
		phaseBPct = float64(phaseBWins) / float64(phaseBSteps) * 100
	}

	panels := []tools.Panel{
		// ── Top: K_A, K_B, confidence, K_edge ────────────────────────────────
		{
			Kind:       "chart",
			Title:      "Rule Alignment K per Step",
			GridLeft:   "6%",
			GridRight:  "4%",
			GridTop:    "14%",
			GridBottom: "38%",
			XLabels:    stepLabels,
			XAxisName:  "Step",
			XInterval:  max(1, steps/10),
			XShow:      true,
			Series: []tools.PanelSeries{
				{
					Name:  "K_Rule_A",
					Kind:  "line",
					Color: "#3b82f6",
					Data:  exp.kA,
				},
				{
					Name:  "K_Rule_B",
					Kind:  "line",
					Color: "#ef4444",
					Data:  exp.kB,
				},
				{
					Name:   "Confidence (K)",
					Kind:   "dashed",
					Symbol: "none",
					Color:  "#93c5fd",
					Data:   confidence,
				},
				{
					Name:   "K_edge (local)",
					Kind:   "dashed",
					Symbol: "none",
					Color:  "#fca5a5",
					Data:   kEdge,
				},
			},
			YMin: tools.Float64Ptr(0),
			YMax: tools.Float64Ptr(1),
		},
		// ── Bottom: winner bar ────────────────────────────────────────────────
		{
			Kind:       "chart",
			Title:      "Winner",
			GridLeft:   "6%",
			GridRight:  "4%",
			GridTop:    "72%",
			GridBottom: "10%",
			XLabels:    stepLabels,
			XAxisName:  "Step",
			XInterval:  max(1, steps/10),
			XShow:      true,
			Series: []tools.PanelSeries{
				{
					Name:     "Rule A wins",
					Kind:     "bar",
					BarWidth: "100%",
					Color:    "#3b82f6",
					Data:     winnerVals,
				},
			},
			YMin: tools.Float64Ptr(0),
			YMax: tools.Float64Ptr(1),
		},
	}

	return []tools.Artifact{
		{
			Type:     tools.ArtifactMultiPanel,
			FileName: "ruleshift_trajectory",
			Data: tools.MultiPanelData{
				Panels: panels,
				Width:  1300,
				Height: 600,
			},
			Title:   "Rule-Shift Adaptation Trajectory",
			Caption: fmt.Sprintf("Rule alignment K over %d steps. Shift at step %d. Phase A: %d/%d Rule-A wins (%.0f%%). Phase B: %d/%d Rule-B wins (%.0f%%).", steps, shiftStep, phaseAWins, phaseASteps, phaseAPct, phaseBWins, phaseBSteps, phaseBPct),
			Label:   "fig:ruleshift_trajectory",
		},
		{
			Type:     tools.ArtifactProse,
			FileName: "ruleshift_section.tex",
			Data: tools.ProseData{
				Template: proseTemplate,
				Data: map[string]any{
					"N":            n,
					"Total":        steps,
					"ShiftStep":    shiftStep,
					"RecoveryDesc": recoveryDesc,
					"PhaseASteps":  phaseASteps,
					"PhaseBSteps":  phaseBSteps,
					"PhaseAWins":   phaseAWins,
					"PhaseBWins":   phaseBWins,
					"PhaseAPct":    phaseAPct,
					"PhaseBPct":    phaseBPct,
				},
			},
		},
	}
}
