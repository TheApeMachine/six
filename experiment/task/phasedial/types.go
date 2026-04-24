package phasedial

// phasedialAssessment returns a standardised assessment paragraph for a
// phasedial experiment based on the overall score.
func phasedialAssessment(score float64) string {
	switch {
	case score > 0.5:
		return `The substrate demonstrated strong performance on this geometric property,
confirming that the invariant holds reliably at this ingestion scale.`
	case score > 0.1:
		return `Partial invariance was observed.  The property holds for a subset of
samples but becomes unreliable under more challenging conditions.`
	default:
		return `The property was not reliably detected at this stage.  The phasedial
experiments require a functional Finalize path to populate the substrate
with compositional data; this infrastructure is being rebuilt during
the current refactoring phase.`
	}
}

// TwoHopTrace records one (α₂, best-C) sample from a two-hop sweep.
type TwoHopTrace struct {
	Alpha2      float64 `json:"alpha2"`
	Gain        float64 `json:"gain"`
	SimCA       float64 `json:"sim_ca"`
	SimCB       float64 `json:"sim_cb"`
	MatchIdx    int     `json:"match_idx"`
	SimCAB      float64 `json:"sim_cab"`
	BalancedSum float64 `json:"balanced_sum"`
	Separation  float64 `json:"separation"`
}

// TwoHopResult aggregates traces and baseline gains for one two-hop experiment run.
type TwoHopResult struct {
	SeedQuery    string        `json:"seed_query"`
	BestMatchB   string        `json:"best_match_b"`
	Traces       []TwoHopTrace `json:"traces"`
	Base1MaxGain float64       `json:"base1_max_gain"`
	Base2MaxGain float64       `json:"base2_max_gain"`
	BestComposed TwoHopTrace   `json:"best_composed"`
}
