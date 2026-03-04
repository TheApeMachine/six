package phasedial

// TwoHopTrace records one (α₂, best-C) sample from a two-hop sweep.
type TwoHopTrace struct {
	Alpha2      float64 `json:"alpha2"`
	Gain        float64 `json:"gain"`
	SimCA       float64 `json:"sim_ca"`
	SimCB       float64 `json:"sim_cb"`
	MatchText   string  `json:"match_text"`
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
