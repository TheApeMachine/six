package semantic

import "github.com/theapemachine/six/pkg/numeric"

/*
Fact represents a Subject-Link-Object triple stored as a resonant Braid.
*/
type Fact struct {
	Subject string
	Link    string
	Object  string
	Phase   numeric.Phase
}
