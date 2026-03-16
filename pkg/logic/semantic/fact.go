package semantic

import "github.com/theapemachine/six/pkg/numeric"

/*
Fact represents a Subject-Link-Object triple stored as a resonant Braid.
Temporal encodes time-axis positioning via a GF(257) phase multiplier.
Negated marks this fact as a destructive interference constraint.
*/
type Fact struct {
	Subject  string
	Link     string
	Object   string
	Phase    numeric.Phase
	Temporal numeric.Phase
	Label    numeric.Phase
	Negated  bool
}


