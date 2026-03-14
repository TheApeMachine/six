package semantic

import (
	context "context"
	"net"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/validate"
)

/*
EngineServer provides zero-shot geometric reasoning via Algebraic Data Cancellation.
It represents the "System API" discussed in Fermat reasoning paradigms.
*/
type EngineServer struct {
	ctx         context.Context
	cancel      context.CancelFunc
	serverSide  net.Conn
	clientSide  net.Conn
	client      Engine
	serverConn  *rpc.Conn
	clientConns map[string]*rpc.Conn
	calc        *numeric.Calculus
	facts       []Fact
	phaseIndex  map[numeric.Phase][]Fact
	braidIndex  map[numeric.Phase][]int
}

type engineOpts func(*EngineServer)

/*
NewEngine instantiates a new Engine for Semantic Algebra.
*/
func NewEngineServer(opts ...engineOpts) *EngineServer {
	eng := &EngineServer{
		calc:       numeric.NewCalculus(),
		facts:      []Fact{},
		phaseIndex: map[numeric.Phase][]Fact{},
		braidIndex: map[numeric.Phase][]int{},
	}

	for _, opt := range opts {
		opt(eng)
	}

	validate.Require(map[string]any{
		"ctx":    eng.ctx,
		"cancel": eng.cancel,
	})

	eng.serverSide, eng.clientSide = net.Pipe()
	eng.client = Engine_ServerToClient(eng)

	eng.serverConn = rpc.NewConn(rpc.NewStreamTransport(
		eng.serverSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(eng.client),
	})

	return eng
}

/*
Client returns a Cap'n Proto client connected to this PrompterServer.
*/
func (server *EngineServer) Client(clientID string) Engine {
	server.clientConns[clientID] = rpc.NewConn(rpc.NewStreamTransport(
		server.clientSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(server.client),
	})

	return server.client
}

/*
Prompt is a remote method that can be called by a client.
*/
func (server *EngineServer) Prompt(ctx context.Context, params Engine_prompt) error {
	return nil
}

/*
Inject stores a new Semantic Fact as a Fermat Braid.
The S-V-O components are hashed to Phases and multiplied.
Returns Phase(0) and does not append if any component sums to zero (unqueryable).
*/
func (eng *EngineServer) Inject(subject, link, object string) numeric.Phase {
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

	idx := len(eng.facts)
	eng.facts = append(eng.facts, fact)
	eng.phaseIndex[po] = append(eng.phaseIndex[po], fact)
	eng.phaseIndex[ps] = append(eng.phaseIndex[ps], fact)
	eng.braidIndex[braid] = append(eng.braidIndex[braid], idx)

	return braid
}

/*
InjectLabel introduces a Cross-Modal Alignment marker to a fact.
By tagging a fact with a specific 'Label Phase', the context can be modulated
simultaneously across dual modalities (e.g., text and corresponding image embeddings)
without changing the core Subject-Link-Object text structure.
*/
func (eng *EngineServer) InjectLabel(subject, link, object string, labelPhase numeric.Phase) numeric.Phase {
	ps := eng.calc.Sum(subject)
	pl := eng.calc.Sum(link)
	po := eng.calc.Sum(object)

	if ps == 0 || pl == 0 || po == 0 || labelPhase == 0 {
		return numeric.Phase(0)
	}

	// The semantic text braid
	braid := eng.calc.Multiply(eng.calc.Multiply(ps, pl), po)

	// Cross-Modal constraint applied via multiplication
	modulatedBraid := eng.calc.Multiply(braid, labelPhase)

	fact := Fact{
		Subject: subject,
		Link:    link,
		Object:  object,
		Phase:   modulatedBraid,
		Label:   labelPhase,
	}

	idx := len(eng.facts)
	eng.facts = append(eng.facts, fact)
	eng.phaseIndex[po] = append(eng.phaseIndex[po], fact)
	eng.phaseIndex[ps] = append(eng.phaseIndex[ps], fact)
	eng.braidIndex[modulatedBraid] = append(eng.braidIndex[modulatedBraid], idx)

	return modulatedBraid
}

/*
QueryObject performs modular inversion to cancel the Subject and Link,
leaving the Resonant Object Phase.
Uses braidIndex for exact fact resolution; Resonate handles merged braids.
*/
func (eng *EngineServer) QueryObject(braid numeric.Phase, subject, link string) (string, numeric.Phase, error) {
	invS, err := eng.calc.Inverse(eng.calc.Sum(subject))
	if err != nil {
		return "", 0, err
	}

	invL, err := eng.calc.Inverse(eng.calc.Sum(link))
	if err != nil {
		return "", 0, err
	}

	target := eng.calc.Multiply(eng.calc.Multiply(braid, invS), invL)

	if name, ok := eng.resolve(braid, target, subject, link, componentObject); ok {
		return name, target, nil
	}

	name, phase := eng.Resonate(target)
	return name, phase, nil
}

/*
QuerySubject performs modular inversion to cancel the Link and Object,
leaving the Resonant Subject Phase.
Uses braidIndex for exact fact resolution; Resonate handles merged braids.
*/
func (eng *EngineServer) QuerySubject(braid numeric.Phase, link, object string) (string, numeric.Phase, error) {
	invL, err := eng.calc.Inverse(eng.calc.Sum(link))
	if err != nil {
		return "", 0, err
	}

	invO, err := eng.calc.Inverse(eng.calc.Sum(object))
	if err != nil {
		return "", 0, err
	}

	target := eng.calc.Multiply(eng.calc.Multiply(braid, invL), invO)

	if name, ok := eng.resolve(braid, target, link, object, componentSubject); ok {
		return name, target, nil
	}

	name, phase := eng.Resonate(target)
	return name, phase, nil
}

/*
componentAxis selects which S-V-O component to extract.
*/
type componentAxis int

const (
	componentSubject componentAxis = iota
	componentLink
	componentObject
)

/*
resolve performs braid-verified, string-verified fact lookup.
Scans the braidIndex bucket for facts matching the braid, verifies the cancel
target equals the desired component's phase, and confirms string identity on
the two known components. Zero-collision by construction.
*/
func (eng *EngineServer) resolve(braid, target numeric.Phase, knownA, knownB string, axis componentAxis) (string, bool) {
	indices := eng.braidIndex[braid]

	for _, factIdx := range indices {
		fact := eng.facts[factIdx]

		switch axis {
		case componentObject:
			if eng.calc.Sum(fact.Object) != target {
				continue
			}

			if fact.Subject == knownA && fact.Link == knownB {
				return fact.Object, true
			}

		case componentSubject:
			if eng.calc.Sum(fact.Subject) != target {
				continue
			}

			if fact.Link == knownA && fact.Object == knownB {
				return fact.Subject, true
			}

		case componentLink:
			if eng.calc.Sum(fact.Link) != target {
				continue
			}

			if fact.Subject == knownA && fact.Object == knownB {
				return fact.Link, true
			}
		}
	}

	return "", false
}

/*
Resonate scans the semantic space to find the string phase that matches
the target phase within a small integer distance (tolerance < 2).
Uses phaseIndex for O(1) exact matches, falls back to scan for nearby phases.
*/
func (eng *EngineServer) Resonate(target numeric.Phase) (string, numeric.Phase) {
	if bucket := eng.phaseIndex[target]; len(bucket) > 0 {
		for _, fact := range bucket {
			po := eng.calc.Sum(fact.Object)
			if target == po {
				return fact.Object, po
			}
			ps := eng.calc.Sum(fact.Subject)
			if target == ps {
				return fact.Subject, ps
			}
		}
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
func (eng *EngineServer) Merge(contexts []numeric.Phase) numeric.Phase {
	var sum numeric.Phase

	for _, phase := range contexts {
		sum = eng.calc.Add(sum, phase)
	}

	return sum
}

/*
diff returns the absolute shortest modular distance between two GF(257) phases.
*/
func (eng *EngineServer) diff(a, b numeric.Phase) uint32 {
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
	return func(eng *EngineServer) {
		eng.Inject(subject, link, object)
	}
}

/*
EngineWithTemporalFact loads a temporally-positioned premise.
*/
func EngineWithTemporalFact(subject, link, object string, temporal numeric.Phase) engineOpts {
	return func(eng *EngineServer) {
		eng.InjectTemporal(subject, link, object, temporal)
	}
}

/*
InjectNegation stores a negative constraint via additive inverse.
The braid phase is (257 - normalPhase), causing destructive interference
when merged with the positive version. Returns the anti-phase.
*/
func (eng *EngineServer) InjectNegation(subject, link, object string) numeric.Phase {
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

	idx := len(eng.facts)
	eng.facts = append(eng.facts, fact)
	eng.braidIndex[antiBraid] = append(eng.braidIndex[antiBraid], idx)

	return antiBraid
}

/*
InjectTemporal stores a fact with a temporal phase multiplier.
The temporal dimension modulates the braid: Phase * G^temporal,
allowing "was in" vs "is in" vs "will be in" disambiguation.
*/
func (eng *EngineServer) InjectTemporal(subject, link, object string, temporal numeric.Phase) numeric.Phase {
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

	idx := len(eng.facts)
	eng.facts = append(eng.facts, fact)
	eng.phaseIndex[po] = append(eng.phaseIndex[po], fact)
	eng.phaseIndex[ps] = append(eng.phaseIndex[ps], fact)
	eng.phaseIndex[pl] = append(eng.phaseIndex[pl], fact)
	eng.braidIndex[timeBraid] = append(eng.braidIndex[timeBraid], idx)

	return timeBraid
}

/*
QueryLink performs modular inversion to cancel Subject and Object,
leaving the Resonant Link Phase. Completes the S-V-O query triad.
Uses braidIndex for exact fact resolution; linear scan handles merged braids.
*/
func (eng *EngineServer) QueryLink(braid numeric.Phase, subject, object string) (string, numeric.Phase, error) {
	invS, err := eng.calc.Inverse(eng.calc.Sum(subject))
	if err != nil {
		return "", 0, err
	}

	invO, err := eng.calc.Inverse(eng.calc.Sum(object))
	if err != nil {
		return "", 0, err
	}

	target := eng.calc.Multiply(eng.calc.Multiply(braid, invS), invO)

	if name, ok := eng.resolve(braid, target, subject, object, componentLink); ok {
		return name, target, nil
	}

	for _, fact := range eng.facts {
		if eng.calc.Sum(fact.Link) == target {
			return fact.Link, target, nil
		}
	}

	return "", target, nil
}

/*
DeBraid extracts individual fact phases from a merged multi-context state.
Given a merged phase M = A + B + C and a known fact phase K, it returns
the residual R = M - K (the remaining merged context without K).
This enables selective cancellation of known components from a superposition.
*/
func (eng *EngineServer) DeBraid(merged, known numeric.Phase) numeric.Phase {
	return eng.calc.Subtract(merged, known)
}

/*
DeBraidFact extracts a specific fact from a merged state by cancelling
its S-V-O braid. Returns the residual merged state containing all
other facts. Returns ErrZeroInverse if the fact components hash to zero.
*/
func (eng *EngineServer) DeBraidFact(merged numeric.Phase, subject, link, object string) (numeric.Phase, error) {
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
func (eng *EngineServer) Interference(a, b numeric.Phase) bool {
	sum := eng.calc.Add(a, b)

	return uint32(sum) <= 2 || uint32(sum) >= numeric.FermatPrime-2
}
