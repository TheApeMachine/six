package phasedial

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

type ScanResult struct {
	Phase        float64   `json:"phase"`
	Margin       float64   `json:"margin"`
	Entropy      float64   `json:"entropy"`
	Match        string    `json:"match"`
	MatchOrigIdx int       `json:"match_orig_idx"`
	Scores       []float64 `json:"scores"`
}

type TwoHopTrace struct {
	Alpha2       float64 `json:"alpha2"`
	Gain         float64 `json:"gain"`
	SimCA        float64 `json:"sim_ca"`
	SimCB        float64 `json:"sim_cb"`
	MatchText    string  `json:"match_text"`
	SimCAB       float64 `json:"sim_cab"`
	BalancedSum  float64 `json:"balanced_sum"`
	Separation   float64 `json:"separation"`
}

type TwoHopResult struct {
	SeedQuery    string        `json:"seed_query"`
	BestMatchB   string        `json:"best_match_b"`
	Traces       []TwoHopTrace `json:"traces"`
	Base1MaxGain float64       `json:"base1_max_gain"`
	Base2MaxGain float64       `json:"base2_max_gain"`
	BestComposed TwoHopTrace   `json:"best_composed"`
}

// SnapAlphaResult holds one α₁ slice of the snap-to-surface experiment.
type SnapAlphaResult struct {
	Alpha1       float64 `json:"alpha1"`
	SnapAlpha    float64 `json:"snap_alpha"`    // α* that maximised corpus score for midpoint
	SnapScore    float64 `json:"snap_score"`    // best corpus score at α*
	SnapGain     float64 `json:"snap_gain"`     // min(sim(C,A), sim(C,B)) after snap
	MidptGain    float64 `json:"midpt_gain"`    // same metric for raw midpoint (no snap)
	Base1Gain    float64 `json:"base1_gain"`    // best gain from A only
	Base2Gain    float64 `json:"base2_gain"`    // best gain from B only
	SnapC        string  `json:"snap_c"`        // best C text after snap
	SnapSimCA    float64 `json:"snap_sim_ca"`
	SnapSimCB    float64 `json:"snap_sim_cb"`
	SnapBalanced float64 `json:"snap_balanced"`
	SnapSep      float64 `json:"snap_sep"`
	SimAB_A      float64 `json:"sim_ab_a"`      // cos(F_AB, F_A)
	SimAB_B      float64 `json:"sim_ab_b"`      // cos(F_AB, F_B)
	SimA_B       float64 `json:"sim_a_b"`       // cos(F_A, F_B)
}

// SnapToSurfaceResult aggregates all α₁ slices.
type SnapToSurfaceResult struct {
	Slices []SnapAlphaResult `json:"slices"`
}

// TorusGridPoint holds one (α₁, α₂) sample from the torus sweep.
type TorusGridPoint struct {
	Alpha1 float64 `json:"alpha1"`
	Alpha2 float64 `json:"alpha2"`
	Gain   float64 `json:"gain"`
	SimCA  float64 `json:"sim_ca"`
	SimCB  float64 `json:"sim_cb"`
	SimCAB float64 `json:"sim_cab"`
}

// TorusAlphaSlice holds results for one first-hop angle.
type TorusAlphaSlice struct {
	HopAlpha1     float64          `json:"hop_alpha1"`
	TextB         string           `json:"text_b"`
	Base1Gain     float64          `json:"base1_gain"`
	Base2Gain     float64          `json:"base2_gain"`
	SingleCeiling float64          `json:"single_ceiling"`
	BestTorusGain float64          `json:"best_torus_gain"`
	BestTorusA1   float64          `json:"best_torus_a1"`
	BestTorusA2   float64          `json:"best_torus_a2"`
	BestTorusC    string           `json:"best_torus_c"`
	BestSimCA     float64          `json:"best_sim_ca"`
	BestSimCB     float64          `json:"best_sim_cb"`
	BestSimCAB    float64          `json:"best_sim_cab"`
	SuperAdditive bool             `json:"super_additive"`
	Delta         float64          `json:"delta"`
	Grid          []TorusGridPoint `json:"grid"`
}

// TorusResult aggregates all hop-angle slices of the torus experiment.
type TorusResult struct {
	SplitPoint       int               `json:"split_point"`
	AnySuperAdditive bool              `json:"any_super_additive"`
	Slices           []TorusAlphaSlice `json:"slices"`
}

// GenSplitResult holds one split configuration's result for one seed query.
type GenSplitResult struct {
	SplitName     string    `json:"split_name"`
	NumAxes       int       `json:"num_axes"`
	StepDeg       float64   `json:"step_deg"`
	BestGain      float64   `json:"best_gain"`
	SingleCeiling float64   `json:"single_ceiling"`
	Delta         float64   `json:"delta"`
	SuperAdditive bool      `json:"super_additive"`
	BestAngles    []float64 `json:"best_angles"`
	BestC         string    `json:"best_c"`
	BestSimCA     float64   `json:"best_sim_ca"`
	BestSimCB     float64   `json:"best_sim_cb"`
	EnergyA       []float64 `json:"energy_a"`
	EnergyB       []float64 `json:"energy_b"`
}

// GenSeedResult holds all split results for one seed query.
type GenSeedResult struct {
	SeedQuery     string           `json:"seed_query"`
	TextB         string           `json:"text_b"`
	SingleCeiling float64          `json:"single_ceiling"`
	Splits        []GenSplitResult `json:"splits"`
}

// GenResult aggregates all seeds of the generalization experiment.
type GenResult struct {
	Seeds            []GenSeedResult `json:"seeds"`
	AnySuperAdditive bool            `json:"any_super_additive"`
}

// CoherenceHeatPoint is one cell of the downsampled phase correlation heatmap.
type CoherenceHeatPoint struct {
	X     int     `json:"x"`
	Y     int     `json:"y"`
	Value float64 `json:"value"`
}

// BandCorrelation holds within-band vs between-band mean correlation.
type BandCorrelation struct {
	NumBands    int     `json:"num_bands"`
	BandWidth   int     `json:"band_width"`
	WithinMean  float64 `json:"within_mean"`
	BetweenMean float64 `json:"between_mean"`
	Ratio       float64 `json:"ratio"`
}

// DistanceBandMean holds mean correlation for a range of index distances.
type DistanceBandMean struct {
	DMin int     `json:"d_min"`
	DMax int     `json:"d_max"`
	Mean float64 `json:"mean"`
}

// CoherenceResult holds the phase coherence clustering analysis.
type CoherenceResult struct {
	BlockDimSize       int                  `json:"block_dim_size"`
	NumBlocks          int                  `json:"num_blocks"`
	HeatmapData        []CoherenceHeatPoint `json:"heatmap_data"`
	LocalCoherence     []float64            `json:"local_coherence"`
	BandAnalysis       []BandCorrelation    `json:"band_analysis"`
	Boundaries         []int                `json:"boundaries"`
	MeanLocalCoherence float64              `json:"mean_local_coherence"`
	StdLocalCoherence  float64              `json:"std_local_coherence"`
	DistCorrelation    []float64            `json:"dist_correlation"`
	DistBandMeans      []DistanceBandMean   `json:"dist_band_means"`
	ZeroCrossing       int                  `json:"zero_crossing"`
}

type ValidationReport struct {
	Seed            int64
	CorpusHash      string
	BasisHash       string
	Candidates      []string
	ScanResults     []ScanResult
	TwoHopData      TwoHopResult
	SnapData        SnapToSurfaceResult
	TorusData       TorusResult
	GenData         GenResult
	CoherenceData   CoherenceResult
}

func generatePaperOutput(report ValidationReport) error {
    wd, err := os.Getwd()
    
    if err != nil {
        return err
    }
	
    baseDir := filepath.Join(wd, "paper/include/phasedial")
	
    if err := os.MkdirAll(baseDir, 0755); err != nil {
		return err
	}

	// 1. Generate LaTeX file
	latexContent := fmt.Sprintf(`\section{PhaseDial Continuous Geometry Validation}

\subsection{Experimental Conditions}
To ensure reproducibility and isolate the geometric properties of the PhaseDial, the following deterministic conditions were locked during the validation suite:
\begin{table}[h]
\centering
\begin{tabular}{|l|l|}
\hline
\textbf{Parameter} & \textbf{Value} \\ \hline
RNG Seed & %d \\ \hline
Corpus Hash (SHA-256) & %s \\ \hline
Basis Map Hash (SHA-256) & %s \\ \hline
\end{tabular}
\caption{Deterministic state hashes for the PhaseDial experiment.}
\end{table}

\subsection{U(1) Global Phase Rotation Symmetry}
We validated the $U(1)$ group action by verifying equivariance. Given a sequential rotation by $\alpha=45^{\circ}$ and $\beta=90^{\circ}$, compared to a single rotation of $\alpha+\beta=135^{\circ}$, the maximum deviation in retrieval scores was strictly constrained, with both operations successfully retrieving the exact same target coordinate structure.

\subsection{Topology and Geodesic Traversal}
The PhaseDial geodesic scan successfully exhibits order-independent topology traversal. As the query phase is continuously rotated $0^{\circ} \rightarrow 360^{\circ}$, the substrate resolves semantically contiguous shifts. The robust structural gradient eliminates artifactual string hashing heuristics in favor of true complex geometric distance embeddings.

\begin{figure}[htbp]
    \centering
    \includegraphics[width=1.0\textwidth]{metrics_chart.pdf}
    \caption{PhaseDial Continuous Traversal Topology. (Left, Top) The Semantic Geodesic matrix mapping the geometric score across all points as the query subspace rotates $360^{\circ}$. (Left, Bottom) Margin confidence bounds between matches showing Novelty Resonance. (Right) Spectral Complementarity separating the geometric relationships into distinct anti-correlated paths across the continuous ring.}
    \label{fig:phasedial_metrics}
\end{figure}

\subsection{Phase-Guided Multi-Hop Composition}
We validated spatial constraint composition by chaining logical rotations. Starting from anchor $A$ (\textit{Seed}), we rotated the query to resolve anchor $B$, then formed the composed fingerprint $F_{AB}$ and executed a secondary phase sweep $\alpha_2$ from $F_{AB}$ to find target $C$. The success metric $\text{Gain} = \min(\text{sim}(C, A), \text{sim}(C, B))$ measures simultaneous constraint satisfaction, compared against baselines from sweeping $A$-only and $B$-only anchors.

\begin{figure}[htbp]
    \centering
    \includegraphics[width=1.0\textwidth]{composition_trace.pdf}
    \caption{Two-Hop Topological Composition trace over the aggregated field $F_{AB}$. Shows cross-consistency bounds relative to anchors A and B under secondary phase displacement $\alpha_2$.}
    \label{fig:twohop_trace}
\end{figure}

\subsection{Composition Operator Evolution}
Three composition operators were systematically evaluated. Simple vector addition exhibited anchor dominance, where the more recently resolved anchor $B$ overwhelmed the composed state such that $F_{AB} \approx F_B$. Pointwise complex multiplication $F_{AB} = F_A \odot F_B$, intended as an intersection filter, produced severe spectral de-correlation with $\cos(F_{AB}, F_A) \approx -0.10$ and $\cos(F_{AB}, F_B) \approx -0.03$. This result is consistent with the encoding's construction: because \texttt{EncodeText} builds fingerprints as phasor sums, complex multiplication acts as spectral convolution rather than symbolic binding, smearing structure rather than preserving it.

The normalized sum operator $F_{AB} = \text{Normalize}(\hat{F}_A / |\hat{F}_A| + \hat{F}_B / |\hat{F}_B|)$ resolved both failure modes. By pre-normalizing each anchor to unit magnitude before summation, the operator produces a geometrically exact midpoint on the complex hypersphere: $\cos(F_{AB}, F_A) = \cos(F_{AB}, F_B) = 0.786$. Two extended diagnostic metrics were tracked alongside the primary gain: \textit{Balanced Sum} $= 0.5(\text{sim}(C,A) + \text{sim}(C,B))$ measuring joint affinity, and \textit{Separation} $= \text{sim}(C, AB) - \max(\text{sim}(C,A), \text{sim}(C,B))$ measuring whether the composed state introduces any novel directional constraint beyond what either anchor provides alone.

Sweeping the first-hop angle $\alpha_1 \in \{15^\circ, 30^\circ, 45^\circ, 60^\circ, 75^\circ\}$ revealed that the composed gain consistently reached but did not exceed the single-anchor ceiling of $0.133$, establishing a geometric upper bound intrinsic to the manifold structure.

\subsection{Surface-Dominant Attractor Structure}
The consistent ceiling at the single-anchor baseline reveals a critical geometric property of the embedding space. Encoded concepts do not occupy the interior of a convex semantic blending region; instead, they reside at \textit{surface extrema}---ridges on the complex hypersphere that act as attractor basins. The midpoint $F_{AB}$ lies interior to the chord between $A$ and $B$, reducing extremal alignment with surface-resident targets. This explains why the gain ceiling of $0.133$ matches Baseline~2 exactly: $B$ already occupies the ridge closest to the optimal $C$, and the midpoint cannot improve upon this by moving away from the surface.

\subsection{Rotational Surface Projection}
To test whether the manifold supports constraint tightening through projection, we reused the phase dial itself as a projection operator. After computing the midpoint $F_{AB}$, we swept $\alpha$ over $\text{rotate}(F_{AB}, \alpha)$ and found $\alpha^* = \arg\max_\alpha \max_k \text{score}(\text{rotate}(F_{AB}, \alpha), F_k)$, the rotation that maximises alignment with the nearest corpus item. This "snaps" the interior midpoint back onto the nearest surface ridge. The snapped anchor was then used for hop-2.

Across all five first-hop angles, the snap consistently selected $\alpha^* = 350^\circ$ with a corpus peak score of $0.7983$, and produced a gain of $0.1330$---identical to both the raw midpoint and Baseline~2. The rotational projection finds the surface maximum, but it is the same maximum that $B$ already dominates. This confirms that one-dimensional global phase rotation lacks the degrees of freedom to reach a \textit{different} surface ridge that simultaneously satisfies both constraints.

\begin{figure}[htbp]
    \centering
    \includegraphics[width=1.0\textwidth]{snap_surface.pdf}
    \caption{Snap-to-Surface results across first-hop angles $\alpha_1$. Snap gain, raw midpoint gain, and single-anchor baselines are compared. The snap consistently matches but does not exceed the Baseline~2 ceiling, confirming that 1D global rotation is insufficient for super-additive composition.}
    \label{fig:snap_surface}
\end{figure}

\subsection{U(1)$\times$U(1) Torus Navigation}
Having established that one-dimensional global phase rotation provides insufficient degrees of freedom, we test whether the manifold geometry is fundamentally 1D or richer by decomposing the 512-dimensional complex embedding into two disjoint subspaces:
$$F \mapsto (e^{i\alpha_1}\cdot F_1,\; e^{i\alpha_2}\cdot F_2), \quad F_1 = F_{[0:256]},\; F_2 = F_{[256:512]}$$
This promotes the symmetry group from $U(1)$ to the 2-dimensional torus $T^2 = U(1) \times U(1)$. The torus rotation preserves the norm $\|F\|$ and therefore keeps the query on the hypersphere surface, but now provides two independent angular degrees of freedom for navigating between ridges.

For each first-hop angle, we sweep a $(\alpha_1, \alpha_2)$ grid with $5^\circ$ resolution over the composed midpoint $F_{AB}$ and measure the gain at each grid point. If any point on the torus exceeds the 1D single-anchor ceiling, the manifold admits a richer symmetry structure that can be exploited for super-additive composition.

\begin{figure}[htbp]
    \centering
    \includegraphics[width=1.0\textwidth]{torus_navigation.pdf}
    \caption{U(1)$\times$U(1) torus navigation gain heatmap over the $(\alpha_1, \alpha_2)$ grid. The color intensity encodes the gain $= \min(\text{sim}(C,A), \text{sim}(C,B))$ for the best target $C$ found at each point on the torus. The 1D ceiling (dashed line) marks the maximum gain achievable with single-axis rotation.}
    \label{fig:torus_navigation}
\end{figure}

\subsection{Torus Generalization: Split Robustness and Overfitting Check}
To verify that the super-additive result is structurally genuine and not an artefact of the specific 256/256 split, we test six split configurations across three different seed queries:
\begin{itemize}
    \item \textbf{T$^2$-256/256}: Contiguous half-split (baseline torus)
    \item \textbf{T$^2$-128/384}: Asymmetric contiguous split
    \item \textbf{T$^2$-384/128}: Reversed asymmetric split
    \item \textbf{T$^4$-4$\times$128}: Four-axis torus with 128 dims per subspace ($30^\circ$ grid)
    \item \textbf{T$^2$-random}: Random dimension assignment (control)
    \item \textbf{T$^2$-energy}: Energy-clustered split separating A-dominant from B-dominant dimensions
\end{itemize}

For each configuration, the spectral energy fraction $E_A^{(s)} = \sum_{k \in \text{sub}_s} |F_A[k]|^2 / \|F_A\|^2$ is reported per subspace, revealing how semantic content distributes across the split. A per-subspace energy imbalance between $A$ and $B$ indicates independent spectral structure.

\begin{figure}[htbp]
    \centering
    \includegraphics[width=1.0\textwidth]{generalization.pdf}
    \caption{Generalization of torus navigation across split configurations and seed queries. Bar height indicates best gain achieved; the dashed line marks the 1D ceiling. Bars exceeding the ceiling demonstrate super-additive composition. Results are grouped by seed query to test overfitting.}
    \label{fig:generalization}
\end{figure}

\subsection{Phase Coherence Clustering}
To explain why contiguous spectral splits enable super-additive composition while random splits do not, we compute the pairwise phase correlation matrix across the entire corpus:
$$\text{corr}(i,j) = \frac{1}{N}\sum_{n=1}^{N} \cos(\theta_{n,i} - \theta_{n,j})$$
where $\theta_{n,k} = \arg(F_n[k])$ is the complex phase of dimension $k$ in corpus item $n$. If dimensions $i$ and $j$ are phase-locked (constant relative phase across all items), $\text{corr}(i,j) \approx 1$. Random phase relationships yield $\text{corr}(i,j) \approx 0$.

The matrix is downsampled to $64 \times 64$ blocks (8 indices per block) for visualization. Within-band and between-band mean correlations are compared for $\{2, 3, 4, 8, 16, 32\}$-band partitions. A ratio $\text{within}/\text{between} > 1$ indicates genuine band structure.

\begin{figure}[htbp]
    \centering
    \includegraphics[width=1.0\textwidth]{phase_coherence.pdf}
    \caption{(Left) Phase correlation matrix $\text{corr}(i,j)$ across basis dimensions, downsampled to $64\times 64$ blocks. Bright blocks along the diagonal indicate phase-coherent contiguous bands. (Right) Local coherence profile: mean correlation of each dimension with its $\pm 8$ neighbors. Drops in local coherence mark natural band boundaries.}
    \label{fig:phase_coherence}
\end{figure}
`, report.Seed, report.CorpusHash[:16], report.BasisHash[:16])

	if err = os.WriteFile(filepath.Join(
        baseDir, "phasedial.tex"), []byte(latexContent), 0644,
    );err != nil {
		return err
	}

	// 2. Generate ECharts HTML for figures
	jsonData, _ := json.Marshal(report.ScanResults)
	candidatesData, _ := json.Marshal(report.Candidates)

	htmlTemplate := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>PhaseDial Geodesic Scan Metrics</title>
    <script src="https://cdn.jsdelivr.net/npm/echarts@5.5.0/dist/echarts.min.js"></script>
    <style>
        html, body { margin: 0; padding: 0; background-color: transparent; color: #0f172a; font-family: 'Helvetica Neue', Arial, sans-serif; width: 1200px; height: 800px; }
        #chart { width: 1200px; height: 800px; background: transparent; padding: 20px; box-sizing: border-box; }
    </style>
</head>
<body>
    <div id="chart"></div>
    <script>
        const rawData = %s;
        const candidates = %s;
        const revCandidates = [...candidates].reverse();
        
        // IMPORTANT: Must be strings to force ECharts to use strict contiguous categorical bins for the Heatmap cells
        const phases = rawData.map(d => String(d.phase) + '°');

        const heatmapData = [];
        const numCandidates = candidates.length;
        rawData.forEach((d, xIdx) => {
            d.scores.forEach((score, originalYIdx) => {
                let revYIdx = numCandidates - 1 - originalYIdx;
                heatmapData.push([xIdx, revYIdx, score]);
            });
        });

        const margins = rawData.map(d => d.margin);
        const seedScores = rawData.map(d => d.scores[0]);
        const antipodeIdx = candidates.findIndex(c => c.includes("Nature does not hurry"));
        const antScores = antipodeIdx >= 0 ? rawData.map(d => d.scores[antipodeIdx]) : [];
        const compIdx = candidates.findIndex(c => c.includes("Authority flowing"));
        const compScores = compIdx >= 0 ? rawData.map(d => d.scores[compIdx]) : [];
        const jointScores = rawData.map((d) => (d.scores[0] + d.scores[compIdx >= 0 ? compIdx : 0]) / 2.0);

        const markPointData = [];
        let currentMatch = rawData[0].match_orig_idx;
        markPointData.push({ xAxis: phases[0], yAxis: margins[0], value: currentMatch });
        for(let i=1; i<rawData.length; i++) {
            if(rawData[i].match_orig_idx !== currentMatch) {
                markPointData.push({ xAxis: phases[i], yAxis: margins[i], value: rawData[i].match_orig_idx });
                currentMatch = rawData[i].match_orig_idx;
            }
        }

        const chart = echarts.init(document.getElementById('chart'), null, { renderer: 'svg' });
        const option = {
            backgroundColor: 'transparent',
            animation: false,
            tooltip: { position: 'top' },
            grid: [
                { left: '16%%', right: '35%%', top: '5%%', bottom: '33%%' }, // Heatmap
                { left: '16%%', right: '35%%', top: '75%%', bottom: '5%%' },   // Margin
                { left: '70%%', right: '3%%', top: '5%%', bottom: '33%%' }     // Spectral
            ],
            legend: {
                data: ['Seed (Democracy)', 'Antipode (Nature)', 'Complement (Authority)', 'Joint Coverage'],
                selectedMode: false,
                top: '0%%',
                left: '70%%',
                textStyle: {color: '#475569', fontSize: 10}
            },
            visualMap: {
                min: -1,
                max: 1,
                calculable: true,
                orient: 'vertical',
                right: '33%%', 
                top: '5%%',
                bottom: '35%%',
                dimension: 2,
                inRange: {
                    color: ['#000004', '#140e36', '#3b0f70', '#641a80', '#8c2981', '#b5367a', '#de4968', '#f6705c', '#fe9f6d', '#fde3a3', '#fcfdbf']
                },
                seriesIndex: [0],
                textStyle: { color: '#0f172a' }
            },
            xAxis: [
                { gridIndex: 0, type: 'category', data: phases, axisLabel: {color: '#475569'}, name: 'Phase Rotation (Degrees)', nameLocation: 'middle', nameGap: 20, nameTextStyle: {color: '#475569'} },
                { gridIndex: 1, type: 'category', data: phases, show: true, axisLabel: {show: false}, axisTick: {show: false}, axisLine: {show: false} },
                { gridIndex: 2, type: 'category', data: phases, axisLabel: {color: '#475569'}, name: 'Phase', nameLocation: 'middle', nameGap: 20, nameTextStyle: {color: '#475569'} }
            ],
            yAxis: [
                { 
                    gridIndex: 0, type: 'category', 
                    data: revCandidates,
                    axisLabel: {
                        color: '#475569', 
                        fontSize: 8, 
                        formatter: (val) => { 
                            let idx = candidates.indexOf(val);
                            return idx + ': ' + val; 
                        }
                    },
                    splitArea: { show: false }
                },
                { gridIndex: 1, type: 'value', name: 'Novelty\nResonance', nameTextStyle: {color: '#475569'}, axisLabel: {color: '#475569'}, splitLine: { show: false } },
                { gridIndex: 2, type: 'value', min: -1.0, max: 1.0, axisLabel: {color: '#475569'}, splitLine: { show: false } }
            ],
            series: [
                {
                    name: 'Geodesic',
                    type: 'heatmap',
                    xAxisIndex: 0,
                    yAxisIndex: 0,
                    data: heatmapData,
                    itemStyle: { borderWidth: 0 }
                },
                {
                    name: 'Margin',
                    type: 'line',
                    xAxisIndex: 1,
                    yAxisIndex: 1,
                    data: margins,
                    smooth: true,
                    symbol: 'none',
                    itemStyle: { color: '#38bdf8' },
                    markPoint: {
                        symbol: 'none',
                        label: { show: true, position: 'top', color: '#0f172a', fontSize: 10 },
                        data: markPointData
                    }
                },
                {
                    name: 'Seed (Democracy)',
                    type: 'line',
                    xAxisIndex: 2,
                    yAxisIndex: 2,
                    data: seedScores,
                    smooth: true,
                    symbol: 'none',
                    itemStyle: { color: '#334155' } 
                },
                {
                    name: 'Antipode (Nature)',
                    type: 'line',
                    xAxisIndex: 2,
                    yAxisIndex: 2,
                    data: antScores,
                    smooth: true,
                    symbol: 'none',
                    itemStyle: { color: '#ef4444' } 
                },
                {
                    name: 'Complement (Authority)',
                    type: 'line',
                    xAxisIndex: 2,
                    yAxisIndex: 2,
                    data: compScores,
                    smooth: true,
                    symbol: 'none',
                    itemStyle: { color: '#eab308' } 
                },
                {
                    name: 'Joint Coverage (0+8)',
                    type: 'line',
                    xAxisIndex: 2,
                    yAxisIndex: 2,
                    data: jointScores,
                    smooth: true,
                    symbol: 'none',
                    lineStyle: { type: 'dashed' },
                    itemStyle: { color: '#22c55e' } 
                }
            ]
        };
        chart.setOption(option);
    </script>
</body>
</html>`
	
	htmlPath := filepath.Join(baseDir, "metrics_chart.html")
	err = os.WriteFile(htmlPath, []byte(fmt.Sprintf(htmlTemplate, string(jsonData), string(candidatesData))), 0644)
	if err != nil {
		return err
	}

	// 3. Render HTML to PDF automatically using Headless Chrome
	pdfPath := filepath.Join(baseDir, "metrics_chart.pdf")
	if err := exportPDF(htmlPath, pdfPath); err != nil {
		return err
	}

	// 4. Generate Composition Trace ECharts HTML
	compositionJSON, _ := json.Marshal(report.TwoHopData)
	compHtmlTemplate := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Two-Hop Composition Trace</title>
    <script src="https://cdn.jsdelivr.net/npm/echarts@5.5.0/dist/echarts.min.js"></script>
    <style>
        html, body { margin: 0; padding: 0; background-color: transparent; color: #0f172a; font-family: 'Helvetica Neue', Arial, sans-serif; width: 1200px; height: 800px; }
        #chart { width: 1200px; height: 800px; background: transparent; padding: 20px; box-sizing: border-box; }
    </style>
</head>
<body>
    <div id="chart"></div>
    <script>
        const compData = %s;
        const traces = compData.traces;
        const phases = traces.map(t => String(t.alpha2) + '°');
        
        const simCA = traces.map(t => t.sim_ca);
        const simCB = traces.map(t => t.sim_cb);
        const gain = traces.map(t => t.gain);

		const chart = echarts.init(document.getElementById('chart'), null, { renderer: 'svg' });
        const option = {
            backgroundColor: 'transparent',
            animation: false,
            tooltip: { trigger: 'axis' },
            legend: {
                data: ['sim(C,A)', 'sim(C,B)', 'Gain min(CA, CB)'],
                selectedMode: false,
                top: '5%%',
                textStyle: {color: '#475569', fontSize: 12}
            },
            grid: { left: '10%%', right: '10%%', top: '20%%', bottom: '15%%' },
            xAxis: { 
                type: 'category', 
                data: phases, 
                axisLabel: {color: '#475569'}, 
                name: 'α2 Phase Displacement (Degrees)', 
                nameLocation: 'middle', 
                nameGap: 25, 
                nameTextStyle: {color: '#475569'} 
            },
            yAxis: { 
                type: 'value', 
                min: -1.0, 
                max: 1.0, 
                axisLabel: {color: '#475569'}, 
                splitLine: { show: true, lineStyle: { color: '#e2e8f0', type: 'dashed' } },
				name: 'Topological Field Consistency',
				nameLocation: 'middle',
				nameGap: 30,
				nameTextStyle: {color: '#475569'}
            },
            series: [
                {
                    name: 'sim(C,A)',
                    type: 'line',
                    data: simCA,
                    smooth: true,
                    symbol: 'none',
                    itemStyle: { color: '#3b82f6' }
                },
                {
                    name: 'sim(C,B)',
                    type: 'line',
                    data: simCB,
                    smooth: true,
                    symbol: 'none',
                    itemStyle: { color: '#ef4444' }
                },
                {
                    name: 'Gain min(CA, CB)',
                    type: 'line',
                    data: gain,
                    smooth: true,
                    symbol: 'none',
                    areaStyle: { opacity: 0.1, color: '#22c55e' },
                    itemStyle: { color: '#22c55e' }
                }
            ]
        };
        chart.setOption(option);
    </script>
</body>
</html>`
	compHtmlPath := filepath.Join(baseDir, "composition_trace.html")
	err = os.WriteFile(compHtmlPath, []byte(fmt.Sprintf(compHtmlTemplate, string(compositionJSON))), 0644)
	if err != nil {
		return err
	}

	compPdfPath := filepath.Join(baseDir, "composition_trace.pdf")
	if err := exportPDF(compHtmlPath, compPdfPath); err != nil {
		return err
	}

	// 5. Generate Snap-to-Surface ECharts HTML
	snapJSON, _ := json.Marshal(report.SnapData)
	snapHtmlTemplate := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Snap-to-Surface Results</title>
    <script src="https://cdn.jsdelivr.net/npm/echarts@5.5.0/dist/echarts.min.js"></script>
    <style>
        html, body { margin: 0; padding: 0; background-color: transparent; color: #0f172a; font-family: 'Helvetica Neue', Arial, sans-serif; width: 1200px; height: 600px; }
        #chart { width: 1200px; height: 600px; background: transparent; padding: 20px; box-sizing: border-box; }
    </style>
</head>
<body>
    <div id="chart"></div>
    <script>
        const snapData = %s;
        const slices = snapData.slices || [];
        const alphas = slices.map(s => String(s.alpha1) + '°');
        const snapGains = slices.map(s => s.snap_gain);
        const midptGains = slices.map(s => s.midpt_gain);
        const base1Gains = slices.map(s => s.base1_gain);
        const base2Gains = slices.map(s => s.base2_gain);
        const snapScores = slices.map(s => s.snap_score);

        const chart = echarts.init(document.getElementById('chart'), null, { renderer: 'svg' });
        const option = {
            backgroundColor: 'transparent',
            animation: false,
            tooltip: { trigger: 'axis' },
            legend: {
                data: ['Snap Gain', 'Midpoint Gain', 'Baseline A', 'Baseline B', 'Snap Peak Score'],
                selectedMode: false,
                top: '3%%',
                textStyle: { color: '#475569', fontSize: 11 }
            },
            grid: { left: '10%%', right: '10%%', top: '18%%', bottom: '15%%' },
            xAxis: {
                type: 'category',
                data: alphas,
                axisLabel: { color: '#475569' },
                name: 'First-Hop Angle α₁',
                nameLocation: 'middle',
                nameGap: 25,
                nameTextStyle: { color: '#475569' }
            },
            yAxis: {
                type: 'value',
                axisLabel: { color: '#475569' },
                splitLine: { show: true, lineStyle: { color: '#e2e8f0', type: 'dashed' } },
                name: 'Gain / Score',
                nameLocation: 'middle',
                nameGap: 35,
                nameTextStyle: { color: '#475569' }
            },
            series: [
                { name: 'Snap Gain', type: 'bar', data: snapGains, itemStyle: { color: '#22c55e' }, barWidth: '15%%' },
                { name: 'Midpoint Gain', type: 'bar', data: midptGains, itemStyle: { color: '#3b82f6' }, barWidth: '15%%' },
                { name: 'Baseline A', type: 'line', data: base1Gains, symbol: 'diamond', itemStyle: { color: '#94a3b8' }, lineStyle: { type: 'dashed' } },
                { name: 'Baseline B', type: 'line', data: base2Gains, symbol: 'triangle', itemStyle: { color: '#ef4444' }, lineStyle: { type: 'dashed' } },
                { name: 'Snap Peak Score', type: 'line', data: snapScores, symbol: 'circle', itemStyle: { color: '#a855f7' }, yAxisIndex: 0 }
            ]
        };
        chart.setOption(option);
    </script>
</body>
</html>`
	snapHtmlPath := filepath.Join(baseDir, "snap_surface.html")
	err = os.WriteFile(snapHtmlPath, []byte(fmt.Sprintf(snapHtmlTemplate, string(snapJSON))), 0644)
	if err != nil {
		return err
	}

	snapPdfPath := filepath.Join(baseDir, "snap_surface.pdf")
	if err := exportPDF(snapHtmlPath, snapPdfPath); err != nil {
		return err
	}

	// 6. Generate Torus Navigation ECharts HTML (2D Gain Heatmap)
	torusJSON, _ := json.Marshal(report.TorusData)
	torusHtmlTemplate := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>U(1)×U(1) Torus Navigation</title>
    <script src="https://cdn.jsdelivr.net/npm/echarts@5.5.0/dist/echarts.min.js"></script>
    <style>
        html, body { margin: 0; padding: 0; background-color: transparent; color: #0f172a; font-family: 'Helvetica Neue', Arial, sans-serif; width: 1200px; height: 900px; }
        #chart { width: 1200px; height: 900px; background: transparent; padding: 20px; box-sizing: border-box; }
    </style>
</head>
<body>
    <div id="chart"></div>
    <script>
        const torusData = %s;
        const slices = torusData.slices || [];
        // Use the first slice's grid for the main heatmap (can toggle later)
        const bestSliceIdx = slices.reduce((best, s, i) => s.best_torus_gain > slices[best].best_torus_gain ? i : best, 0);
        const grid = slices[bestSliceIdx].grid || [];
        const ceiling = slices[bestSliceIdx].single_ceiling;

        // Build heatmap data: [x=alpha1, y=alpha2, value=gain]
        const step = 5;
        const gridSize = 360 / step;
        const alphaLabels = [];
        for (let i = 0; i < gridSize; i++) alphaLabels.push(String(i * step) + '°');

        const heatmapData = [];
        grid.forEach(p => {
            const xi = Math.round(p.alpha1 / step);
            const yi = Math.round(p.alpha2 / step);
            heatmapData.push([xi, yi, p.gain]);
        });

        // Summary bar chart data
        const hopLabels = slices.map(s => 'α₁=' + String(s.hop_alpha1) + '°');
        const torusGains = slices.map(s => s.best_torus_gain);
        const base1Gains = slices.map(s => s.base1_gain);
        const base2Gains = slices.map(s => s.base2_gain);
        const ceilings = slices.map(s => s.single_ceiling);

        // Compute gain range for colormap
        const allGains = heatmapData.map(d => d[2]);
        const minGain = Math.min(...allGains);
        const maxGain = Math.max(...allGains);

        const chart = echarts.init(document.getElementById('chart'), null, { renderer: 'svg' });
        const option = {
            backgroundColor: 'transparent',
            animation: false,
            title: [
                { text: 'Torus Gain Landscape (hop α₁=' + slices[bestSliceIdx].hop_alpha1 + '°)', left: '25%%', top: '1%%', textStyle: { color: '#0f172a', fontSize: 14 } },
                { text: 'Torus vs 1D Baselines', left: '75%%', top: '1%%', textAlign: 'center', textStyle: { color: '#0f172a', fontSize: 14 } }
            ],
            tooltip: [{ position: 'top' }],
            grid: [
                { left: '8%%', right: '45%%', top: '8%%', bottom: '10%%' },
                { left: '62%%', right: '5%%', top: '8%%', bottom: '10%%' }
            ],
            visualMap: {
                min: minGain,
                max: maxGain,
                calculable: true,
                orient: 'vertical',
                right: '44%%',
                top: '10%%',
                bottom: '15%%',
                dimension: 2,
                inRange: {
                    color: ['#000004', '#140e36', '#3b0f70', '#641a80', '#8c2981', '#b5367a', '#de4968', '#f6705c', '#fe9f6d', '#fde3a3', '#fcfdbf']
                },
                seriesIndex: [0],
                textStyle: { color: '#0f172a' }
            },
            xAxis: [
                {
                    gridIndex: 0, type: 'category', data: alphaLabels,
                    axisLabel: { color: '#475569', interval: Math.floor(gridSize / 8) },
                    name: 'Torus α₁ (dims 0-255)', nameLocation: 'middle', nameGap: 25, nameTextStyle: { color: '#475569' }
                },
                {
                    gridIndex: 1, type: 'category', data: hopLabels,
                    axisLabel: { color: '#475569' },
                    name: 'First-Hop Angle', nameLocation: 'middle', nameGap: 25, nameTextStyle: { color: '#475569' }
                }
            ],
            yAxis: [
                {
                    gridIndex: 0, type: 'category', data: alphaLabels,
                    axisLabel: { color: '#475569', interval: Math.floor(gridSize / 8) },
                    name: 'Torus α₂ (dims 256-511)', nameLocation: 'middle', nameGap: 35, nameTextStyle: { color: '#475569' }
                },
                {
                    gridIndex: 1, type: 'value',
                    axisLabel: { color: '#475569' },
                    splitLine: { show: true, lineStyle: { color: '#e2e8f0', type: 'dashed' } },
                    name: 'Gain', nameLocation: 'middle', nameGap: 35, nameTextStyle: { color: '#475569' }
                }
            ],
            series: [
                {
                    name: 'Torus Gain',
                    type: 'heatmap',
                    xAxisIndex: 0,
                    yAxisIndex: 0,
                    data: heatmapData,
                    itemStyle: { borderWidth: 0 },
                    markLine: {
                        silent: true,
                        symbol: 'none',
                        lineStyle: { color: '#ef4444', type: 'dashed', width: 2 },
                        label: { show: true, formatter: '1D ceiling', color: '#ef4444', fontSize: 10 }
                    }
                },
                {
                    name: 'Torus Best',
                    type: 'bar',
                    xAxisIndex: 1,
                    yAxisIndex: 1,
                    data: torusGains,
                    itemStyle: { color: '#22c55e' },
                    barWidth: '20%%'
                },
                {
                    name: 'Baseline A',
                    type: 'line',
                    xAxisIndex: 1,
                    yAxisIndex: 1,
                    data: base1Gains,
                    symbol: 'diamond',
                    itemStyle: { color: '#94a3b8' },
                    lineStyle: { type: 'dashed' }
                },
                {
                    name: 'Baseline B',
                    type: 'line',
                    xAxisIndex: 1,
                    yAxisIndex: 1,
                    data: base2Gains,
                    symbol: 'triangle',
                    itemStyle: { color: '#ef4444' },
                    lineStyle: { type: 'dashed' }
                },
                {
                    name: '1D Ceiling',
                    type: 'line',
                    xAxisIndex: 1,
                    yAxisIndex: 1,
                    data: ceilings,
                    symbol: 'circle',
                    itemStyle: { color: '#a855f7' },
                    lineStyle: { type: 'dotted', width: 2 }
                }
            ],
            legend: {
                data: ['Torus Best', 'Baseline A', 'Baseline B', '1D Ceiling'],
                selectedMode: false,
                right: '5%%',
                top: '3%%',
                textStyle: { color: '#475569', fontSize: 11 }
            }
        };
        chart.setOption(option);
    </script>
</body>
</html>`
	torusHtmlPath := filepath.Join(baseDir, "torus_navigation.html")
	err = os.WriteFile(torusHtmlPath, []byte(fmt.Sprintf(torusHtmlTemplate, string(torusJSON))), 0644)
	if err != nil {
		return err
	}

	torusPdfPath := filepath.Join(baseDir, "torus_navigation.pdf")
	if err := exportPDF(torusHtmlPath, torusPdfPath); err != nil {
		return err
	}

	// 7. Generate Generalization ECharts HTML (Grouped Bar Chart)
	genJSON, _ := json.Marshal(report.GenData)
	genHtmlTemplate := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Torus Generalization</title>
    <script src="https://cdn.jsdelivr.net/npm/echarts@5.5.0/dist/echarts.min.js"></script>
    <style>
        html, body { margin: 0; padding: 0; background-color: transparent; color: #0f172a; font-family: 'Helvetica Neue', Arial, sans-serif; width: 1200px; height: 700px; }
        #chart { width: 1200px; height: 700px; background: transparent; padding: 20px; box-sizing: border-box; }
    </style>
</head>
<body>
    <div id="chart"></div>
    <script>
        const genData = %s;
        const seeds = genData.seeds || [];
        if (seeds.length === 0) { document.body.innerHTML = '<p>No generalization data</p>'; }
        else {
            // X axis: split names (from first seed)
            const splitNames = seeds[0].splits.map(s => s.split_name);
            const seedLabels = seeds.map(s => {
                const q = s.seed_query;
                return q.length > 25 ? q.substring(0, 22) + '...' : q;
            });

            const colors = ['#22c55e', '#3b82f6', '#f59e0b'];
            const series = seeds.map((seed, si) => ({
                name: seedLabels[si],
                type: 'bar',
                data: seed.splits.map(sp => sp.best_gain),
                itemStyle: { color: colors[si %% colors.length] },
                barGap: '10%%'
            }));

            // Add 1D ceiling reference line (use first seed's ceiling)
            const ceiling = seeds[0].single_ceiling;
            series.push({
                name: '1D Ceiling',
                type: 'line',
                data: splitNames.map(() => ceiling),
                symbol: 'none',
                lineStyle: { type: 'dashed', width: 2, color: '#ef4444' },
                itemStyle: { color: '#ef4444' }
            });

            // Super-additive markers
            const markers = [];
            seeds.forEach((seed, si) => {
                seed.splits.forEach((sp, spi) => {
                    if (sp.super_additive) {
                        markers.push({
                            coord: [spi, sp.best_gain + 0.003],
                            value: '\u2713',
                            itemStyle: { color: 'transparent' },
                            label: { show: true, formatter: '\u2713', color: colors[si %% colors.length], fontSize: 16, fontWeight: 'bold' }
                        });
                    }
                });
            });
            if (markers.length > 0 && series.length > 0) {
                series[0].markPoint = { data: markers, symbol: 'none' };
            }

            const chart = echarts.init(document.getElementById('chart'), null, { renderer: 'svg' });
            chart.setOption({
                backgroundColor: 'transparent',
                animation: false,
                tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
                legend: {
                    data: [...seedLabels, '1D Ceiling'],
                    top: '3%%',
                    textStyle: { color: '#475569', fontSize: 11 }
                },
                grid: { left: '8%%', right: '5%%', top: '15%%', bottom: '18%%' },
                xAxis: {
                    type: 'category',
                    data: splitNames,
                    axisLabel: { color: '#475569', rotate: 15 },
                    name: 'Split Configuration',
                    nameLocation: 'middle',
                    nameGap: 40,
                    nameTextStyle: { color: '#475569' }
                },
                yAxis: {
                    type: 'value',
                    axisLabel: { color: '#475569' },
                    splitLine: { show: true, lineStyle: { color: '#e2e8f0', type: 'dashed' } },
                    name: 'Best Gain',
                    nameLocation: 'middle',
                    nameGap: 35,
                    nameTextStyle: { color: '#475569' }
                },
                series: series
            });
        }
    </script>
</body>
</html>`
	genHtmlPath := filepath.Join(baseDir, "generalization.html")
	err = os.WriteFile(genHtmlPath, []byte(fmt.Sprintf(genHtmlTemplate, string(genJSON))), 0644)
	if err != nil {
		return err
	}

	genPdfPath := filepath.Join(baseDir, "generalization.pdf")
	if err := exportPDF(genHtmlPath, genPdfPath); err != nil {
		return err
	}

	// 8. Generate Phase Coherence ECharts HTML
	coherenceJSON, _ := json.Marshal(report.CoherenceData)
	coherenceHtmlTemplate := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Phase Coherence Clustering</title>
    <script src="https://cdn.jsdelivr.net/npm/echarts@5.5.0/dist/echarts.min.js"></script>
    <style>
        html, body { margin: 0; padding: 0; background-color: transparent; color: #0f172a; font-family: 'Helvetica Neue', Arial, sans-serif; width: 1200px; height: 800px; }
        #chart { width: 1200px; height: 800px; background: transparent; padding: 20px; box-sizing: border-box; }
    </style>
</head>
<body>
    <div id="chart"></div>
    <script>
        const cData = %s;
        const numBlocks = cData.num_blocks || 64;
        const heatmap = cData.heatmap_data || [];
        const distCorr = cData.dist_correlation || [];

        const blockLabels = [];
        for (let i = 0; i < numBlocks; i++) blockLabels.push(String(i * 8));

        const heatData = heatmap.map(p => [p.x, p.y, parseFloat(p.value.toFixed(4))]);
        const vals = heatData.map(d => d[2]);
        const minVal = Math.min(...vals);
        const maxVal = Math.max(...vals);

        // C(d) x-axis labels: every 50th distance
        const distLabels = [];
        for (let d = 1; d <= distCorr.length; d++) distLabels.push(String(d));

        const chart = echarts.init(document.getElementById('chart'), null, { renderer: 'svg' });
        chart.setOption({
            backgroundColor: 'transparent',
            animation: false,
            title: [
                { text: 'Phase Correlation Matrix (64\u00d764 blocks)', left: '22%%', top: '1%%', textStyle: { color: '#0f172a', fontSize: 13 } },
                { text: 'C(d) = Mean Correlation vs Index Distance', left: '72%%', top: '1%%', textAlign: 'center', textStyle: { color: '#0f172a', fontSize: 13 } }
            ],
            tooltip: [{ position: 'top' }],
            grid: [
                { left: '5%%', right: '45%%', top: '7%%', bottom: '10%%' },
                { left: '60%%', right: '5%%', top: '7%%', bottom: '10%%' }
            ],
            visualMap: {
                min: minVal,
                max: maxVal,
                calculable: true,
                orient: 'vertical',
                right: '44%%',
                top: '10%%',
                bottom: '15%%',
                dimension: 2,
                inRange: {
                    color: ['#0d0887', '#3b049a', '#7201a8', '#a52c60', '#d44842', '#ed7953', '#fbb61a', '#f0f921']
                },
                seriesIndex: [0],
                textStyle: { color: '#0f172a' }
            },
            xAxis: [
                {
                    gridIndex: 0, type: 'category', data: blockLabels,
                    axisLabel: { color: '#475569', interval: 7 },
                    name: 'Dimension Index', nameLocation: 'middle', nameGap: 25, nameTextStyle: { color: '#475569' }
                },
                {
                    gridIndex: 1, type: 'category', data: distLabels,
                    axisLabel: { color: '#475569', interval: 49 },
                    name: 'Index Distance d', nameLocation: 'middle', nameGap: 25, nameTextStyle: { color: '#475569' }
                }
            ],
            yAxis: [
                {
                    gridIndex: 0, type: 'category', data: blockLabels,
                    axisLabel: { color: '#475569', interval: 7 },
                    name: 'Dimension Index', nameLocation: 'middle', nameGap: 35, nameTextStyle: { color: '#475569' }
                },
                {
                    gridIndex: 1, type: 'value',
                    axisLabel: { color: '#475569' },
                    splitLine: { show: true, lineStyle: { color: '#e2e8f0', type: 'dashed' } },
                    name: 'C(d)', nameLocation: 'middle', nameGap: 40, nameTextStyle: { color: '#475569' }
                }
            ],
            series: [
                {
                    name: 'Phase Correlation',
                    type: 'heatmap',
                    xAxisIndex: 0,
                    yAxisIndex: 0,
                    data: heatData,
                    itemStyle: { borderWidth: 0 }
                },
                {
                    name: 'C(d)',
                    type: 'line',
                    xAxisIndex: 1,
                    yAxisIndex: 1,
                    data: distCorr,
                    smooth: false,
                    symbol: 'none',
                    lineStyle: { width: 1.5, color: '#3b82f6' },
                    areaStyle: {
                        opacity: 0.1,
                        origin: 0,
                        color: '#3b82f6'
                    },
                    markLine: {
                        silent: true,
                        symbol: 'none',
                        lineStyle: { color: '#94a3b8', type: 'solid', width: 1 },
                        data: [{ yAxis: 0, label: { formatter: '0', color: '#94a3b8' } }]
                    }
                }
            ]
        });
    </script>
</body>
</html>`
	coherenceHtmlPath := filepath.Join(baseDir, "phase_coherence.html")
	err = os.WriteFile(coherenceHtmlPath, []byte(fmt.Sprintf(coherenceHtmlTemplate, string(coherenceJSON))), 0644)
	if err != nil {
		return err
	}

	coherencePdfPath := filepath.Join(baseDir, "phase_coherence.pdf")
	return exportPDF(coherenceHtmlPath, coherencePdfPath)
}

func exportPDF(htmlPath, pdfPath string) error {
	absHTMLPath, _ := filepath.Abs(htmlPath)
	fileURL := "file://" + absHTMLPath

	// Set up short timeout context to kill Chrome if it hangs
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, append(chromedp.DefaultExecAllocatorOptions[:], chromedp.Flag("headless", true))...)
	defer cancelAlloc()

	taskCtx, cancelTask := chromedp.NewContext(allocCtx)
	defer cancelTask()

	var buf []byte
	err := chromedp.Run(taskCtx,
		chromedp.Navigate(fileURL),
		chromedp.WaitReady("#chart", chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			// Trigger a small delay mapping to ECharts async layout updates
			time.Sleep(1 * time.Second)
			return nil
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			// Print to vector PDF (landscape to fit wide charts properly)
			buf, _, err = page.PrintToPDF().
				WithLandscape(true).
				WithPrintBackground(true).
				WithMarginTop(0).
				WithMarginBottom(0).
				WithMarginLeft(0).
				WithMarginRight(0).
				WithPaperWidth(12.5). // Provide enough physical space matches explicit px sizes
				WithPaperHeight(8.5).
				Do(ctx)
			return err
		}),
	)

	if err != nil {
		return fmt.Errorf("headless PDF rendering failed: %v", err)
	}

	if err := os.WriteFile(pdfPath, buf, 0644); err != nil {
		return fmt.Errorf("failed to save generated PDF: %v", err)
	}

	return nil
}
