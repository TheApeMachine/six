package vm

/*
ProjectionMode controls whether the human-facing overlay layers are wired into
the Machine. The native prompt path does not need grammar, semantic facts, or
cantilever assistance to retrieve spans; those are optional projection tools for
experiments and evaluation.
*/
type ProjectionMode uint8

const ProjectionDisabled ProjectionMode = 0

const (
	ProjectionIngest ProjectionMode = 1 << iota
	ProjectionPrompt
	ProjectionAll = ProjectionIngest | ProjectionPrompt
)

/*
Enabled reports whether a specific projection stage is active.
*/
func (mode ProjectionMode) Enabled(stage ProjectionMode) bool {
	if stage == ProjectionDisabled {
		return mode == ProjectionDisabled
	}

	return mode&stage == stage
}


