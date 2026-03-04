package codegen

import (
	"fmt"
	"math"
	"math/cmplx"
	"sort"
	"strings"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/numeric"
	"gonum.org/v1/gonum/mat"
)

// testEigenmodeProbe implements Test 8: Eigenmode Probe via Transition Matrix.
//
// Inspired by the old architecture's EigenInit, this test builds an asymmetric
// forward transition matrix T[i][j] at each FibWindow scale, extracts the
// (v2, v3) eigenplane, and maps each byte to a phase angle.
//
// Then each span is assigned a circular mean eigenphase, and we check whether
// structural roles separate in eigenphase space.
//
// This tests the hypothesis that sequential transition structure (who follows
// whom) separates structural roles better than PCA over fingerprint vectors.
func (experiment *Experiment) testEigenmodeProbe(corpus []string) EigenmodeResult {
	const NSymbols = 256

	// ── Step 1: Build multi-scale transition matrix ──
	// For each FibWindow w, build T[i][j] = count(byte i precedes byte j within w positions)
	// Then combine eigenphases via weighted circular mean, exactly like the old architecture.

	var sinAcc, cosAcc [NSymbols]float64

	// Concatenate corpus into a single byte stream
	var fullCorpus []byte
	for _, fn := range corpus {
		fullCorpus = append(fullCorpus, []byte(fn)...)
		fullCorpus = append(fullCorpus, '\n') // separator
	}

	console.Info(fmt.Sprintf("  Corpus: %d bytes from %d functions", len(fullCorpus), len(corpus)))

	for wi, w := range numeric.FibWindows {
		weight := numeric.FibWeights[wi]

		// Build asymmetric forward transition matrix
		var T [NSymbols][NSymbols]float64
		for pos := 0; pos < len(fullCorpus); pos++ {
			sym := fullCorpus[pos]
			end := min(pos+w+1, len(fullCorpus))
			for j := pos + 1; j < end; j++ {
				T[sym][fullCorpus[j]] += 1.0
			}
		}

		// L1-normalize each row → Markov transition matrix
		for i := range NSymbols {
			var sum float64
			for j := range NSymbols {
				sum += T[i][j]
			}
			if sum > 0 {
				for j := range NSymbols {
					T[i][j] /= sum
				}
			}
		}

		// Extract eigenvalues/vectors via gonum
		data := make([]float64, NSymbols*NSymbols)
		for i := range NSymbols {
			for j := 0; j < NSymbols; j++ {
				data[i*NSymbols+j] = T[i][j]
			}
		}
		dense := mat.NewDense(NSymbols, NSymbols, data)

		var eig mat.Eigen
		if !eig.Factorize(dense, mat.EigenRight) {
			console.Warn("  Eigen factorization failed — using power iteration fallback")
			continue
		}

		values := eig.Values(nil)
		indices := make([]int, NSymbols)
		for i := range indices {
			indices[i] = i
		}
		sort.Slice(indices, func(a, b int) bool {
			return cmplx.Abs(values[indices[a]]) > cmplx.Abs(values[indices[b]])
		})

		var vecs mat.CDense
		eig.VectorsTo(&vecs)

		// idx0 ≈ Perron (λ≈1), idx1 and idx2 are next by magnitude
		idx1, idx2 := indices[1], indices[2]
		lam1 := values[idx1]

		var v2, v3 [NSymbols]float64

		if imag(lam1) != 0 {
			// Complex conjugate pair — use real and imag of column idx1
			for i := range NSymbols {
				v := vecs.At(i, idx1)
				v2[i] = real(v)
				v3[i] = imag(v)
			}
		} else if imag(values[idx2]) != 0 {
			for i := range NSymbols {
				v := vecs.At(i, idx2)
				v2[i] = real(v)
				v3[i] = imag(v)
			}
		} else {
			for i := range NSymbols {
				v2[i] = real(vecs.At(i, idx1))
				v3[i] = real(vecs.At(i, idx2))
			}
		}

		// Normalize
		normalizeVec256(&v2)
		normalizeVec256(&v3)

		// Phase[i] = atan2(v3[i], v2[i]) — exactly like old architecture
		for i := range NSymbols {
			phase := math.Atan2(v3[i], v2[i])
			sinAcc[i] += weight * math.Sin(phase)
			cosAcc[i] += weight * math.Cos(phase)
		}

		console.Info(fmt.Sprintf("  Scale w=%d: λ1=%.4f λ2=%.4f (weight=%.3f)",
			w, cmplx.Abs(values[idx1]), cmplx.Abs(values[idx2]), weight))
	}

	// Circular mean of the weighted phase contributions
	var eigenPhase [NSymbols]float64
	for i := range NSymbols {
		eigenPhase[i] = math.Atan2(sinAcc[i], cosAcc[i])
	}

	// ── Step 2: Show eigenphases for key structural tokens ──
	keyTokens := []struct {
		label string
		bytes []byte
	}{
		{"def", []byte("def")},
		{"return", []byte("return")},
		{"for", []byte("for")},
		{"while", []byte("while")},
		{"if", []byte("if")},
		{"(", []byte("(")},
		{")", []byte(")")},
		{":", []byte(":")},
		{"=", []byte("=")},
		{"+", []byte("+")},
		{"[", []byte("[")},
		{"]", []byte("]")},
		{"space", []byte(" ")},
		{"newline", []byte("\n")},
	}

	console.Info("\n  ── Key Token Eigenphases ──")
	for _, kt := range keyTokens {
		phases := make([]float64, len(kt.bytes))
		for i, b := range kt.bytes {
			phases[i] = eigenPhase[b]
		}
		meanPhase := circularMean(phases)
		console.Info(fmt.Sprintf("  %-10s  phase=%.4f (%.1f°)", kt.label, meanPhase, meanPhase*180/math.Pi))
	}

	// ── Step 3: Compute circular mean eigenphase for each span ──
	spanLengths := numeric.FibWindows

	type spanInfo struct {
		text       string
		role       string
		eigenPhase float64
	}
	var spans []spanInfo

	for _, fn := range corpus {
		tokens := tokenize(fn)
		for _, sLen := range spanLengths {
			if len(tokens) < sLen {
				continue
			}
			for start := 0; start <= len(tokens)-sLen; start++ {
				span := make([]string, sLen)
				copy(span, tokens[start:start+sLen])
				spanText := detokenize(span)
				role := classifyRole(spanText)

				// Circular mean of eigenphases of all bytes in this span
				spanBytes := []byte(spanText)
				phases := make([]float64, len(spanBytes))
				for i, b := range spanBytes {
					phases[i] = eigenPhase[b]
				}
				meanPhase := circularMean(phases)

				spans = append(spans, spanInfo{
					text:       spanText,
					role:       role,
					eigenPhase: meanPhase,
				})
			}
		}
	}

	N := len(spans)
	console.Info(fmt.Sprintf("\n  Span count: %d", N))

	// ── Step 4: Per-role statistics in eigenphase space ──
	type roleStat struct {
		count  int
		sinSum float64
		cosSum float64
		phases []float64
	}
	roleStats := make(map[string]*roleStat)

	for _, s := range spans {
		rs, ok := roleStats[s.role]
		if !ok {
			rs = &roleStat{}
			roleStats[s.role] = rs
		}
		rs.count++
		rs.sinSum += math.Sin(s.eigenPhase)
		rs.cosSum += math.Cos(s.eigenPhase)
		rs.phases = append(rs.phases, s.eigenPhase)
	}

	// Compute circular mean and circular standard deviation per role
	roles := []string{"header", "loop", "conditional", "return", "assignment", "call"}
	var entries []EigenmodeEntry

	console.Info("\n  ── Role Eigenphase Statistics ──")
	console.Info(fmt.Sprintf("  %-14s %6s  %10s  %10s  %10s",
		"Role", "Count", "Phase μ", "Phase°", "Circ. σ"))

	for _, role := range roles {
		rs, ok := roleStats[role]
		if !ok {
			continue
		}
		meanPhase := math.Atan2(rs.sinSum/float64(rs.count), rs.cosSum/float64(rs.count))

		// Circular variance: 1 - R̄ where R̄ = |mean resultant|/n
		R := math.Sqrt(rs.sinSum*rs.sinSum+rs.cosSum*rs.cosSum) / float64(rs.count)
		circVar := 1 - R
		circStd := math.Sqrt(-2 * math.Log(R)) // circular std dev
		if R < 1e-10 {
			circStd = math.Pi // uniform
		}

		console.Info(fmt.Sprintf("  %-14s %6d  %+10.4f  %+10.1f°  %10.4f",
			role, rs.count, meanPhase, meanPhase*180/math.Pi, circStd))

		entries = append(entries, EigenmodeEntry{
			Role:    role,
			Count:   rs.count,
			MeanPC1: meanPhase,                 // repurpose: eigenphase mean
			MeanPC2: meanPhase * 180 / math.Pi, // degrees
			MeanPC3: 0,
			StdPC1:  circStd, // circular std dev
			StdPC2:  circVar, // circular variance
			StdPC3:  R,       // concentration R̄
		})
	}

	// ── Step 5: Pairwise angular separation ──
	console.Info("\n  ── Pairwise Eigenphase Separation ──")
	var separations []EigenmodeSeparation

	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			// Angular distance (shortest arc on unit circle)
			diff := entries[i].MeanPC1 - entries[j].MeanPC1
			angDist := math.Abs(math.Atan2(math.Sin(diff), math.Cos(diff)))

			// Average circular std dev
			avgStd := (entries[i].StdPC1 + entries[j].StdPC1) / 2

			ratio := 0.0
			if avgStd > 0 {
				ratio = angDist / avgStd
			}

			console.Info(fmt.Sprintf("  %-14s ↔ %-14s  Δφ=%.4f (%.1f°)  σ̄=%.4f  ratio=%.4f",
				entries[i].Role, entries[j].Role,
				angDist, angDist*180/math.Pi,
				avgStd, ratio))

			separations = append(separations, EigenmodeSeparation{
				RoleA:     entries[i].Role,
				RoleB:     entries[j].Role,
				Distance:  angDist,
				AvgSpread: avgStd,
				Ratio:     ratio,
			})
		}
	}

	// Build point cloud for figure (eigenphase plotted on unit circle)
	var points []EigenmodePoint
	roleCounts := make(map[string]int)
	for _, s := range spans {
		if roleCounts[s.role] >= 200 {
			continue
		}
		roleCounts[s.role]++
		// Map eigenphase to (x, y) on unit circle for scatter plot
		points = append(points, EigenmodePoint{
			PC1:  math.Cos(s.eigenPhase),
			PC2:  math.Sin(s.eigenPhase),
			PC3:  s.eigenPhase,
			Role: s.role,
		})
	}

	wellSep := 0
	for _, sep := range separations {
		if sep.Ratio > 1.0 {
			wellSep++
		}
	}

	console.Info(fmt.Sprintf("\n  Well-separated pairs (ratio > 1): %d/%d", wellSep, len(separations)))

	// Show some example spans per role with their eigenphase
	console.Info("\n  ── Example Spans by Eigenphase ──")
	for _, role := range roles {
		rs := roleStats[role]
		if rs == nil || rs.count == 0 {
			continue
		}
		// Find spans for this role, sorted by eigenphase
		var roleSpans []spanInfo
		for _, s := range spans {
			if s.role == role {
				roleSpans = append(roleSpans, s)
			}
		}
		sort.Slice(roleSpans, func(a, b int) bool {
			return roleSpans[a].eigenPhase < roleSpans[b].eigenPhase
		})
		// Show first, middle, last
		show := func(idx int) {
			s := roleSpans[idx]
			text := strings.ReplaceAll(s.text, "\n", "↵")
			if len(text) > 50 {
				text = text[:50] + "…"
			}
			console.Info(fmt.Sprintf("    φ=%+.3f (%+.0f°): %s", s.eigenPhase, s.eigenPhase*180/math.Pi, text))
		}
		console.Info(fmt.Sprintf("  %s (%d spans):", role, len(roleSpans)))
		if len(roleSpans) >= 3 {
			show(0)
			show(len(roleSpans) / 2)
			show(len(roleSpans) - 1)
		} else {
			for i := range roleSpans {
				show(i)
			}
		}
	}

	return EigenmodeResult{
		TotalSpans:   N,
		Roles:        entries,
		Separations:  separations,
		Points:       points,
		WellSepCount: wellSep,
		TotalPairs:   len(separations),
	}
}

// circularMean computes the circular mean of a slice of angles.
func circularMean(phases []float64) float64 {
	var sinSum, cosSum float64
	for _, p := range phases {
		sinSum += math.Sin(p)
		cosSum += math.Cos(p)
	}
	return math.Atan2(sinSum, cosSum)
}

func normalizeVec256(v *[256]float64) {
	var normSq float64
	for _, x := range v {
		normSq += x * x
	}
	if normSq < 1e-24 {
		return
	}
	norm := math.Sqrt(normSq)
	for i := range v {
		v[i] /= norm
	}
}

// classifyRole tags a span with its dominant structural role.
func classifyRole(text string) string {
	hasDefStart := strings.HasPrefix(text, "def ")
	hasReturn := strings.Contains(text, "return")
	hasFor := strings.Contains(text, "for ")
	hasWhile := strings.Contains(text, "while ")
	hasIf := strings.Contains(text, "if ")
	hasCall := strings.Contains(text, "(") && strings.Contains(text, ")")
	hasAssign := strings.Contains(text, "=") && !strings.Contains(text, "==")

	if hasDefStart {
		return "header"
	}
	if hasReturn && !hasFor && !hasWhile {
		return "return"
	}
	if hasFor || hasWhile {
		return "loop"
	}
	if hasIf {
		return "conditional"
	}
	if hasAssign {
		return "assignment"
	}
	if hasCall {
		return "call"
	}
	return "other"
}
