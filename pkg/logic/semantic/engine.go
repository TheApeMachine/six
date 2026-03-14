package semantic

import "github.com/theapemachine/six/pkg/numeric"

/*
Engine provides zero-shot geometric reasoning via Algebraic Data Cancellation.
It represents the "System API" discussed in Fermat reasoning paradigms.
*/
type Engine struct {
	calc       *numeric.Calculus
	facts      []Fact
	phaseIndex map[numeric.Phase][]Fact
}

type engineOpts func(*Engine)

/*
NewEngine instantiates a new Engine for Semantic Algebra.
*/
func NewEngine(opts ...engineOpts) *Engine {
	eng := &Engine{
		calc:       numeric.NewCalculus(),
		facts:      []Fact{},
		phaseIndex: map[numeric.Phase][]Fact{},
	}

	for _, opt := range opts {
		opt(eng)
	}

	return eng
}

/*
Inject stores a new Semantic Fact as a Fermat Braid.
The S-V-O components are hashed to Phases and multiplied.
Returns Phase(0) and does not append if any component sums to zero (unqueryable).
*/
func (eng *Engine) Inject(subject, link, object string) numeric.Phase {
	ps := eng.calc.Sum(subject)
	pl := eng.calc.Sum(link)
	po := eng.calc.Sum(object)

	if ps == 0 || pl == 0 || po == 0 {
		return numeric.Phase(0)
	}

	braid := eng.calc.Multiply(eng.calc.Multiply(ps, pl), po)

	fact := Fact{
		Subject: subject,
		Link:    link,
		Object:  object,
		Phase:   braid,
	}
	eng.facts = append(eng.facts, fact)
	eng.phaseIndex[po] = append(eng.phaseIndex[po], fact)
	eng.phaseIndex[ps] = append(eng.phaseIndex[ps], fact)

	return braid
}

/*
QueryObject performs modular inversion to cancel the Subject and Link,
leaving the Resonant Object Phase.
*/
func (eng *Engine) QueryObject(braid numeric.Phase, subject, link string) (string, numeric.Phase, error) {
	invS, err := eng.calc.Inverse(eng.calc.Sum(subject))
	if err != nil {
		return "", 0, err
	}
	invL, err := eng.calc.Inverse(eng.calc.Sum(link))
	if err != nil {
		return "", 0, err
	}

	target := eng.calc.Multiply(eng.calc.Multiply(braid, invS), invL)
	s, p := eng.Resonate(target)
	return s, p, nil
}

/*
QuerySubject performs modular inversion to cancel the Link and Object,
leaving the Resonant Subject Phase.
*/
func (eng *Engine) QuerySubject(braid numeric.Phase, link, object string) (string, numeric.Phase, error) {
	invL, err := eng.calc.Inverse(eng.calc.Sum(link))
	if err != nil {
		return "", 0, err
	}
	invO, err := eng.calc.Inverse(eng.calc.Sum(object))
	if err != nil {
		return "", 0, err
	}

	target := eng.calc.Multiply(eng.calc.Multiply(braid, invL), invO)
	s, p := eng.Resonate(target)
	return s, p, nil
}

/*
Resonate scans the semantic space to find the string phase that matches
the target phase within a small integer distance (tolerance < 2).
Uses phaseIndex for O(1) exact matches, falls back to scan for nearby phases.
*/
func (eng *Engine) Resonate(target numeric.Phase) (string, numeric.Phase) {
	if bucket := eng.phaseIndex[target]; len(bucket) > 0 {
		fact := bucket[0]
		po := eng.calc.Sum(fact.Object)
		if target == po {
			return fact.Object, po
		}
		return fact.Subject, eng.calc.Sum(fact.Subject)
	}

	var bestMatch string
	var bestPhase numeric.Phase
	var minDiff uint32 = numeric.FermatPrime

	for _, fact := range eng.facts {
		po := eng.calc.Sum(fact.Object)
		ps := eng.calc.Sum(fact.Subject)

		if diff := eng.diff(target, po); diff < minDiff {
			minDiff = diff
			bestMatch = fact.Object
			bestPhase = po
		}

		if diff := eng.diff(target, ps); diff < minDiff {
			minDiff = diff
			bestMatch = fact.Subject
			bestPhase = ps
		}
	}

	if minDiff <= 2 {
		return bestMatch, bestPhase
	}

	return "", target
}

/*
Merge creates a Multi-Tonal semantic context by summing multiple Fermat paths.
Addition preserves independent states in GF(257).
*/
func (eng *Engine) Merge(contexts []numeric.Phase) numeric.Phase {
	var sum numeric.Phase

	for _, phase := range contexts {
		sum = eng.calc.Add(sum, phase)
	}

	return sum
}

/*
diff returns the absolute shortest modular distance between two GF(257) phases.
*/
func (eng *Engine) diff(a, b numeric.Phase) uint32 {
	diff := int32(a) - int32(b)

	if diff < 0 {
		diff = -diff
	}

	if diff > int32(numeric.FermatPrime)/2 {
		diff = int32(numeric.FermatPrime) - diff
	}

	return uint32(diff)
}

/*
EngineWithFact loads a semantic premise at instantiation time.
*/
func EngineWithFact(subject, link, object string) engineOpts {
	return func(eng *Engine) {
		eng.Inject(subject, link, object)
	}
}

/*
EngineWithTemporalFact loads a temporally-positioned premise.
*/
func EngineWithTemporalFact(subject, link, object string, temporal numeric.Phase) engineOpts {
	return func(eng *Engine) {
		eng.InjectTemporal(subject, link, object, temporal)
	}
}

/*
InjectNegation stores a negative constraint via additive inverse.
The braid phase is (257 - normalPhase), causing destructive interference
when merged with the positive version. Returns the anti-phase.
*/
func (eng *Engine) InjectNegation(subject, link, object string) numeric.Phase {
	ps := eng.calc.Sum(subject)
	pl := eng.calc.Sum(link)
	po := eng.calc.Sum(object)

	if ps == 0 || pl == 0 || po == 0 {
		return numeric.Phase(0)
	}

	normalBraid := eng.calc.Multiply(eng.calc.Multiply(ps, pl), po)
	antiBraid := eng.calc.Subtract(numeric.Phase(0), normalBraid)

	fact := Fact{
		Subject: subject,
		Link:    link,
		Object:  object,
		Phase:   antiBraid,
		Negated: true,
	}

	eng.facts = append(eng.facts, fact)

	return antiBraid
}

/*
InjectTemporal stores a fact with a temporal phase multiplier.
The temporal dimension modulates the braid: Phase * G^temporal,
allowing "was in" vs "is in" vs "will be in" disambiguation.
*/
func (eng *Engine) InjectTemporal(subject, link, object string, temporal numeric.Phase) numeric.Phase {
	ps := eng.calc.Sum(subject)
	pl := eng.calc.Sum(link)
	po := eng.calc.Sum(object)

	if ps == 0 || pl == 0 || po == 0 {
		return numeric.Phase(0)
	}

	spatialBraid := eng.calc.Multiply(eng.calc.Multiply(ps, pl), po)
	timeBraid := eng.calc.Multiply(
		spatialBraid,
		eng.calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32(temporal)),
	)

	fact := Fact{
		Subject:  subject,
		Link:     link,
		Object:   object,
		Phase:    timeBraid,
		Temporal: temporal,
	}

	eng.facts = append(eng.facts, fact)
	eng.phaseIndex[po] = append(eng.phaseIndex[po], fact)
	eng.phaseIndex[ps] = append(eng.phaseIndex[ps], fact)

	return timeBraid
}

/*
QueryLink performs modular inversion to cancel Subject and Object,
leaving the Resonant Link Phase. Completes the S-V-O query triad.
*/
func (eng *Engine) QueryLink(braid numeric.Phase, subject, object string) (string, numeric.Phase, error) {
	invS, err := eng.calc.Inverse(eng.calc.Sum(subject))
	if err != nil {
		return "", 0, err
	}

	invO, err := eng.calc.Inverse(eng.calc.Sum(object))
	if err != nil {
		return "", 0, err
	}

	target := eng.calc.Multiply(eng.calc.Multiply(braid, invS), invO)

	for _, fact := range eng.facts {
		if eng.calc.Sum(fact.Link) == target {
			return fact.Link, target, nil
		}
	}

	var bestMatch string
	var bestPhase numeric.Phase
	var minDiff uint32 = numeric.FermatPrime

	for _, fact := range eng.facts {
		pl := eng.calc.Sum(fact.Link)

		if diff := eng.diff(target, pl); diff < minDiff {
			minDiff = diff
			bestMatch = fact.Link
			bestPhase = pl
		}
	}

	if minDiff <= 2 {
		return bestMatch, bestPhase, nil
	}

	return "", target, nil
}

/*
DeBraid extracts individual fact phases from a merged multi-context state.
Given a merged phase M = A + B + C and a known fact phase K, it returns
the residual R = M - K (the remaining merged context without K).
This enables selective cancellation of known components from a superposition.
*/
func (eng *Engine) DeBraid(merged, known numeric.Phase) numeric.Phase {
	return eng.calc.Subtract(merged, known)
}

/*
DeBraidFact extracts a specific fact from a merged state by cancelling
its S-V-O braid. Returns the residual merged state containing all
other facts. Returns ErrZeroInverse if the fact components hash to zero.
*/
func (eng *Engine) DeBraidFact(merged numeric.Phase, subject, link, object string) (numeric.Phase, error) {
	ps := eng.calc.Sum(subject)
	pl := eng.calc.Sum(link)
	po := eng.calc.Sum(object)

	if ps == 0 || pl == 0 || po == 0 {
		return 0, numeric.ErrZeroInverse
	}

	factPhase := eng.calc.Multiply(eng.calc.Multiply(ps, pl), po)

	return eng.calc.Subtract(merged, factPhase), nil
}

/*
Interference returns true when two phases destructively cancel within
tolerance. Used to detect when a negated fact has successfully
nullified its positive counterpart in a merged context.
*/
func (eng *Engine) Interference(a, b numeric.Phase) bool {
	sum := eng.calc.Add(a, b)

	return uint32(sum) <= 2 || uint32(sum) >= numeric.FermatPrime-2
}
