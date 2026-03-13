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

/*
Engine provides zero-shot geometric reasoning via Algebraic Data Cancellation.
It represents the "System API" discussed in Fermat reasoning paradigms.
*/
type Engine struct {
	calc  *numeric.Calculus
	facts []Fact
	err   error
}

type engineOpts func(*Engine)

/*
NewEngine instantiates a new Engine for Semantic Algebra.
*/
func NewEngine(opts ...engineOpts) *Engine {
	eng := &Engine{
		calc:  numeric.NewCalculus(),
		facts: []Fact{},
	}

	for _, opt := range opts {
		opt(eng)
	}

	return eng
}

/*
Inject stores a new Semantic Fact as a Fermat Braid.
The S-V-O components are hashed to Phases and multiplied.
*/
func (eng *Engine) Inject(subject, link, object string) numeric.Phase {
	ps := eng.calc.Sum(subject)
	pl := eng.calc.Sum(link)
	po := eng.calc.Sum(object)

	braid := eng.calc.Multiply(eng.calc.Multiply(ps, pl), po)

	eng.facts = append(eng.facts, Fact{
		Subject: subject,
		Link:    link,
		Object:  object,
		Phase:   braid,
	})

	return braid
}

/*
QueryObject performs modular inversion to cancel the Subject and Link,
leaving the Resonant Object Phase.
*/
func (eng *Engine) QueryObject(braid numeric.Phase, subject, link string) (string, numeric.Phase) {
	invS := eng.calc.Inverse(eng.calc.Sum(subject))
	invL := eng.calc.Inverse(eng.calc.Sum(link))

	target := eng.calc.Multiply(eng.calc.Multiply(braid, invS), invL)

	return eng.Resonate(target)
}

/*
QuerySubject performs modular inversion to cancel the Link and Object,
leaving the Resonant Subject Phase.
*/
func (eng *Engine) QuerySubject(braid numeric.Phase, link, object string) (string, numeric.Phase) {
	invL := eng.calc.Inverse(eng.calc.Sum(link))
	invO := eng.calc.Inverse(eng.calc.Sum(object))

	target := eng.calc.Multiply(eng.calc.Multiply(braid, invL), invO)

	return eng.Resonate(target)
}

/*
Resonate scans the semantic space to find the string phase that matches
the target phase within a small integer distance (tolerance < 2).
It simulates a spatial/popcount filter inside the 257 GF limits.
*/
func (eng *Engine) Resonate(target numeric.Phase) (string, numeric.Phase) {
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
