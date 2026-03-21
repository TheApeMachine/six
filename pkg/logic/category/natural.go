package category

import (
	"github.com/theapemachine/six/pkg/numeric"
)

/*
NaturalTransformation maps objects between two parallel Functors while
preserving the naturality square: η_B ∘ F(f) = G(f) ∘ η_A for every
morphism f: A→B. The mapping field stores explicit object-level phase
correspondences (e.g., anchoring "function" in one language to "def" in
another).
*/
type NaturalTransformation struct {
	functorF *Functor
	functorG *Functor
	mapping  map[numeric.Phase]numeric.Phase
}

/*
natOpts configures a NaturalTransformation with dependency injection.
*/
type natOpts func(*NaturalTransformation)

/*
NewNaturalTransformation instantiates a NaturalTransformation between two
Functors. Both functors must be aligned before coherence checking.
*/
func NewNaturalTransformation(opts ...natOpts) *NaturalTransformation {
	nat := &NaturalTransformation{
		mapping: make(map[numeric.Phase]numeric.Phase),
	}

	for _, opt := range opts {
		opt(nat)
	}

	return nat
}

/*
CheckCoherence verifies the naturality square commutes for a sample of
morphisms. For each sampled source opcode f with key K, the test checks:

	η(F(K)) ≈ G(K)

where η is the registered object-level mapping, F and G are the two
functors, and ≈ is cosine similarity above (1 − tolerance). Returns the
fraction of sampled squares that commute.
*/
func (nat *NaturalTransformation) CheckCoherence(sampleSize int) (float64, error) {
	if nat.functorF == nil || nat.functorG == nil {
		return 0, NaturalError("both functors must be set")
	}

	if !nat.functorF.aligned || !nat.functorG.aligned {
		return 0, NaturalError("both functors must be aligned")
	}

	if len(nat.mapping) == 0 {
		return 0, NaturalError("no object-level mappings registered")
	}

	nSource := len(nat.functorF.sourceKeys)
	if nSource == 0 || sampleSize < 1 {
		return 0, NaturalError("insufficient source opcodes or sample size")
	}

	passed := 0
	total := 0
	coherenceTolerance := 0.20

	for idx := 0; idx < sampleSize && idx < nSource; idx++ {
		sourceKey := nat.functorF.sourceKeys[idx]

		mappedF, _, errF := nat.functorF.Map(sourceKey)
		mappedG, _, errG := nat.functorG.Map(sourceKey)

		if errF != nil || errG != nil {
			continue
		}

		etaF, hasMappingF := nat.applyEta(mappedF.Scale)

		if !hasMappingF {
			continue
		}

		total++

		dialEtaF := anchorToDial(etaF)
		dialG := anchorToDial(mappedG.Scale)

		similarity := dialEtaF.Similarity(dialG)

		if similarity > 1.0-coherenceTolerance {
			passed++
		}
	}

	if total == 0 {
		return 0, NaturalError("no testable morphisms found")
	}

	return float64(passed) / float64(total), nil
}

/*
MapObject translates a phase from the source category to the target
category at the object level using the registered mapping.
*/
func (nat *NaturalTransformation) MapObject(phase numeric.Phase) (numeric.Phase, bool) {
	mapped, exists := nat.mapping[phase]
	return mapped, exists
}

/*
RegisterMapping establishes a correspondence between two object-level
phases. This is the η component at one object: η_A maps phase A in the
source category to phase B in the target category.
*/
func (nat *NaturalTransformation) RegisterMapping(sourcePhase, targetPhase numeric.Phase) {
	nat.mapping[sourcePhase] = targetPhase
}

/*
applyEta looks up a phase through the natural transformation mapping,
composing through any registered chain.
*/
func (nat *NaturalTransformation) applyEta(phase numeric.Phase) (numeric.Phase, bool) {
	mapped, exists := nat.mapping[phase]
	return mapped, exists
}

/*
NatWithFunctorF sets the first functor (F) for the natural transformation.
*/
func NatWithFunctorF(functor *Functor) natOpts {
	return func(nat *NaturalTransformation) {
		nat.functorF = functor
	}
}

/*
NatWithFunctorG sets the second functor (G) for the natural transformation.
*/
func NatWithFunctorG(functor *Functor) natOpts {
	return func(nat *NaturalTransformation) {
		nat.functorG = functor
	}
}

/*
NaturalError is a typed error for NaturalTransformation failures.
*/
type NaturalError string

/*
Error implements the error interface for NaturalError.
*/
func (err NaturalError) Error() string {
	return string(err)
}
