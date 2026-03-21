package category

import (
	"math"

	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/numeric/geometry"
	config "github.com/theapemachine/six/pkg/system/core"
)

/*
Functor maps morphisms between two MacroIndex categories via Procrustes-
aligned PhaseDial embeddings. The Align step computes the optimal rotation
R between embedding spaces using shared anchors as correspondence pairs;
after alignment, Map becomes a nearest-neighbour lookup in the rotated space.
*/
type Functor struct {
	source      *macro.MacroIndexServer
	target      *macro.MacroIndexServer
	rotation    *geometry.ProcrustesResult
	sourceEmbed []geometry.PhaseDial
	targetEmbed []geometry.PhaseDial
	sourceKeys  []macro.AffineKey
	targetKeys  []macro.AffineKey
	aligned     bool
}

/*
functorOpts configures a Functor with dependency injection.
*/
type functorOpts func(*Functor)

/*
NewFunctor instantiates a Functor between two MacroIndex categories.
Source and target must be set via option functions before calling Align.
*/
func NewFunctor(opts ...functorOpts) *Functor {
	functor := &Functor{}

	for _, opt := range opts {
		opt(functor)
	}

	return functor
}

/*
Align builds the Procrustes rotation between the source and target
MacroIndex embedding spaces. Both indices must have hardened opcodes,
and they must share at least one anchor by name to establish geometric
correspondence. The anchor embeddings serve as the paired rows for
the orthogonal Procrustes solver.
*/
func (functor *Functor) Align() error {
	if functor.source == nil || functor.target == nil {
		return FunctorError("source or target MacroIndex not set")
	}

	sourceOpcodes := functor.source.AvailableHardened()
	targetOpcodes := functor.target.AvailableHardened()

	functor.sourceEmbed = make([]geometry.PhaseDial, len(sourceOpcodes))
	functor.sourceKeys = make([]macro.AffineKey, len(sourceOpcodes))

	for idx, opcode := range sourceOpcodes {
		functor.sourceKeys[idx] = opcode.Key
		functor.sourceEmbed[idx] = EmbedKey(opcode.Key)
	}

	functor.targetEmbed = make([]geometry.PhaseDial, len(targetOpcodes))
	functor.targetKeys = make([]macro.AffineKey, len(targetOpcodes))

	for idx, opcode := range targetOpcodes {
		functor.targetKeys[idx] = opcode.Key
		functor.targetEmbed[idx] = EmbedKey(opcode.Key)
	}

	sourceAnchors := functor.source.AvailableAnchors()
	targetAnchors := functor.target.AvailableAnchors()

	targetByName := make(map[string]*macro.AnchorRecord, len(targetAnchors))
	for _, anchor := range targetAnchors {
		targetByName[anchor.Name] = anchor
	}

	var sourceRows, targetRows []geometry.PhaseDial

	for _, srcAnchor := range sourceAnchors {
		tgtAnchor, exists := targetByName[srcAnchor.Name]
		if !exists {
			continue
		}

		sourceRows = append(sourceRows, anchorToDial(srcAnchor.Phase))
		targetRows = append(targetRows, anchorToDial(tgtAnchor.Phase))
	}

	if len(sourceRows) == 0 {
		return FunctorError("no shared anchors between source and target")
	}

	nDim := config.Numeric.NBasis
	nSamples := len(sourceRows)

	matA := make([][]float64, nSamples)
	matB := make([][]float64, nSamples)

	for sample := 0; sample < nSamples; sample++ {
		matA[sample] = dialToReal(sourceRows[sample], nDim)
		matB[sample] = dialToReal(targetRows[sample], nDim)
	}

	result, procErr := geometry.Procrustes(matA, matB, nSamples, nDim)
	if procErr != nil {
		return procErr
	}

	functor.rotation = result
	functor.aligned = true

	return nil
}

/*
Map translates a source AffineKey to the nearest target AffineKey
via the Procrustes-aligned embedding space. Returns the matched target
opcode, the cosine distance (lower = better), and an error if the
functor is not yet aligned or has no target opcodes.
*/
func (functor *Functor) Map(sourceKey macro.AffineKey) (*macro.MacroOpcode, float64, error) {
	if !functor.aligned {
		return nil, 0, FunctorError("functor not aligned; call Align first")
	}

	if len(functor.targetKeys) == 0 {
		return nil, 0, FunctorError("target index has no hardened opcodes")
	}

	srcDial := EmbedKey(sourceKey)
	nDim := config.Numeric.NBasis
	srcReal := dialToReal(srcDial, nDim)

	rotated := make([]float64, nDim)
	for dim := 0; dim < nDim; dim++ {
		var sum float64

		for inner := 0; inner < nDim; inner++ {
			sum += functor.rotation.R[dim][inner] * srcReal[inner]
		}

		rotated[dim] = sum
	}

	bestIdx := 0
	bestSim := math.Inf(-1)

	for idx, tgtDial := range functor.targetEmbed {
		tgtReal := dialToReal(tgtDial, nDim)
		sim := cosineSimilarity(rotated, tgtReal)

		if sim > bestSim {
			bestSim = sim
			bestIdx = idx
		}
	}

	matchedKey := functor.targetKeys[bestIdx]
	opcode, found := functor.target.FindOpcode(matchedKey)

	if !found {
		return nil, 0, FunctorError("matched key not found in target index")
	}

	distance := 1.0 - bestSim

	return opcode, distance, nil
}

/*
Validate checks composition preservation on a sample of morphism pairs.
For sampled (f, g), verifies F(g∘f) ≈ F(g)∘F(f) within the alignment
tolerance. Returns the fraction of pairs that satisfy the functor law.
*/
func (functor *Functor) Validate(sampleSize int) (float64, error) {
	if !functor.aligned {
		return 0, FunctorError("functor not aligned; call Align first")
	}

	nSource := len(functor.sourceKeys)
	if nSource < 2 || sampleSize < 1 {
		return 0, FunctorError("insufficient source opcodes or sample size for validation")
	}

	passed := 0
	total := 0
	compositionTolerance := 0.15

	for idx := 0; idx < sampleSize && idx < nSource-1; idx++ {
		keyF := functor.sourceKeys[idx]
		keyG := functor.sourceKeys[(idx+1)%nSource]

		composedKey := composeKeys(keyF, keyG)

		mappedF, _, errF := functor.Map(keyF)
		mappedG, _, errG := functor.Map(keyG)
		mappedComposed, _, errC := functor.Map(composedKey)

		if errF != nil || errG != nil || errC != nil {
			continue
		}

		composedTarget := composeKeys(mappedF.Key, mappedG.Key)
		composedDial := EmbedKey(composedTarget)
		mappedDial := EmbedKey(mappedComposed.Key)

		coherence := composedDial.Similarity(mappedDial)
		total++

		if coherence > 1.0-compositionTolerance {
			passed++
		}
	}

	if total == 0 {
		return 0, FunctorError("no valid morphism pairs sampled")
	}

	return float64(passed) / float64(total), nil
}

/*
EmbedKey delegates to macro.EmbedKey which is the canonical projection of an
AffineKey into PhaseDial space. Kept here as a convenience alias so existing
callers in the category package don't need updating.
*/
func EmbedKey(key macro.AffineKey) geometry.PhaseDial {
	return macro.EmbedKey(key)
}

/*
anchorToDial converts a scalar GF(8191) Phase into a PhaseDial by
spreading the phase uniformly across all dimensions with prime-frequency
modulation. This produces a rotationally-distinct embedding per anchor.
*/
func anchorToDial(phase numeric.Phase) geometry.PhaseDial {
	dial := geometry.NewPhaseDial()
	nDim := config.Numeric.NBasis
	baseAngle := float64(phase) * (2.0 * math.Pi / float64(numeric.FieldPrime))

	for k := 0; k < nDim; k++ {
		omega := float64(numeric.Primes[k])
		angle := baseAngle * omega * 0.01
		dial[k] = complex(math.Cos(angle), math.Sin(angle))
	}

	return dial.CopyAndNormalize()
}

/*
dialToReal extracts the real parts of a PhaseDial into a float64 slice
for use with the real-valued Procrustes solver.
*/
func dialToReal(dial geometry.PhaseDial, nDim int) []float64 {
	out := make([]float64, nDim)

	for k := 0; k < nDim && k < len(dial); k++ {
		out[k] = real(dial[k])
	}

	return out
}

/*
cosineSimilarity computes the cosine similarity between two real vectors.
*/
func cosineSimilarity(vecA, vecB []float64) float64 {
	var dot, normA, normB float64

	for idx := range vecA {
		dot += vecA[idx] * vecB[idx]
		normA += vecA[idx] * vecA[idx]
		normB += vecB[idx] * vecB[idx]
	}

	denom := math.Sqrt(normA) * math.Sqrt(normB)

	if denom == 0 {
		return 0
	}

	return dot / denom
}

/*
composeKeys XORs two AffineKeys block-wise, which in the GF(8191) affine
geometry corresponds to composing two delta operators.
*/
func composeKeys(keyA, keyB macro.AffineKey) macro.AffineKey {
	var composed macro.AffineKey

	for blk := range composed {
		composed[blk] = keyA[blk] ^ keyB[blk]
	}

	return composed
}

/*
FunctorWithSource sets the source MacroIndexServer for the Functor.
*/
func FunctorWithSource(source *macro.MacroIndexServer) functorOpts {
	return func(functor *Functor) {
		functor.source = source
	}
}

/*
FunctorWithTarget sets the target MacroIndexServer for the Functor.
*/
func FunctorWithTarget(target *macro.MacroIndexServer) functorOpts {
	return func(functor *Functor) {
		functor.target = target
	}
}

/*
FunctorError is a typed error for Functor operations.
*/
type FunctorError string

/*
Error implements the error interface for FunctorError.
*/
func (err FunctorError) Error() string {
	return string(err)
}
