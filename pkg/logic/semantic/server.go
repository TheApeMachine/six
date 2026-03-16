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
		clientConns: map[string]*rpc.Conn{},
		calc:        numeric.NewCalculus(),
		facts:       []Fact{},
		phaseIndex:  map[numeric.Phase][]Fact{},
		braidIndex:  map[numeric.Phase][]int{},
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
Client returns a Cap'n Proto client connected to this EngineServer.
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
Close shuts down the RPC connections and underlying net.Pipe,
unblocking goroutines stuck on pipe reads.
*/
func (server *EngineServer) Close() error {
	if server.serverConn != nil {
		_ = server.serverConn.Close()
		server.serverConn = nil
	}

	for clientID, conn := range server.clientConns {
		if conn != nil {
			_ = conn.Close()
		}
		delete(server.clientConns, clientID)
	}

	if server.serverSide != nil {
		_ = server.serverSide.Close()
		server.serverSide = nil
	}
	if server.clientSide != nil {
		_ = server.clientSide.Close()
		server.clientSide = nil
	}
	if server.cancel != nil {
		server.cancel()
	}

	return nil
}

/*
Prompt is a remote method that can be called by a client.
*/
func (server *EngineServer) Prompt(ctx context.Context, params Engine_prompt) error {
	return nil
}

/*
Inject implements Engine_Server. Stores an S-V-O fact as a Fermat Braid and
returns the braid phase.
*/
func (server *EngineServer) Inject(ctx context.Context, call Engine_inject) error {
	args := call.Args()

	subject, err := args.Subject()
	if err != nil {
		return err
	}

	link, err := args.Link()
	if err != nil {
		return err
	}

	object, err := args.Object()
	if err != nil {
		return err
	}

	braid := server.InjectFact(subject, link, object)

	res, err := call.AllocResults()
	if err != nil {
		return err
	}

	res.SetBraid(uint32(braid))

	return nil
}

/*
Query implements Engine_Server. Performs algebraic cancellation to resolve
an unknown S-V-O component. Axis: 0=Object, 1=Subject, 2=Link.
*/
func (server *EngineServer) Query(ctx context.Context, call Engine_query) error {
	args := call.Args()

	braid := numeric.Phase(args.Braid())

	knownA, err := args.KnownA()
	if err != nil {
		return err
	}

	knownB, err := args.KnownB()
	if err != nil {
		return err
	}

	axis := args.Axis()

	var name string
	var phase numeric.Phase
	var queryErr error

	switch componentAxis(axis) {
	case componentObject:
		name, phase, queryErr = server.QueryObject(braid, knownA, knownB)
	case componentSubject:
		name, phase, queryErr = server.QuerySubject(braid, knownA, knownB)
	case componentLink:
		name, phase, queryErr = server.QueryLink(braid, knownA, knownB)
	}

	if queryErr != nil {
		return queryErr
	}

	res, err := call.AllocResults()
	if err != nil {
		return err
	}

	res.SetPhase(uint32(phase))

	return res.SetName(name)
}

/*
InjectFact stores a new Semantic Fact as a Fermat Braid.
The S-V-O components are hashed to Phases and multiplied.
Returns Phase(0) and does not append if any component sums to zero.
*/
func (eng *EngineServer) InjectFact(subject, link, object string) numeric.Phase {
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
QueryObject performs modular inversion to cancel the Subject and Link,
leaving the Resonant Object Phase.
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
QueryLink performs modular inversion to cancel Subject and Object,
leaving the Resonant Link Phase.
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
the target phase. Uses phaseIndex for O(1) exact matches.
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
EngineWithContext adds a context to the Engine.
*/
func EngineWithContext(ctx context.Context) engineOpts {
	return func(eng *EngineServer) {
		eng.ctx, eng.cancel = context.WithCancel(ctx)
	}
}

/*
EngineWithFact loads a semantic premise at instantiation time.
*/
func EngineWithFact(subject, link, object string) engineOpts {
	return func(eng *EngineServer) {
		eng.InjectFact(subject, link, object)
	}
}

/*
InjectLabel introduces a Cross-Modal Alignment marker to a fact.
*/
func (eng *EngineServer) InjectLabel(subject, link, object string, labelPhase numeric.Phase) numeric.Phase {
	ps := eng.calc.Sum(subject)
	pl := eng.calc.Sum(link)
	po := eng.calc.Sum(object)

	if ps == 0 || pl == 0 || po == 0 || labelPhase == 0 {
		return numeric.Phase(0)
	}

	braid := eng.calc.Multiply(eng.calc.Multiply(ps, pl), po)
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
InjectNegation stores a negative constraint via additive inverse.
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
EngineWithTemporalFact loads a temporally-positioned premise.
*/
func EngineWithTemporalFact(subject, link, object string, temporal numeric.Phase) engineOpts {
	return func(eng *EngineServer) {
		eng.InjectTemporal(subject, link, object, temporal)
	}
}

/*
DeBraid extracts individual fact phases from a merged multi-context state.
*/
func (eng *EngineServer) DeBraid(merged, known numeric.Phase) numeric.Phase {
	return eng.calc.Subtract(merged, known)
}

/*
DeBraidFact extracts a specific fact from a merged state by cancelling
its S-V-O braid.
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
Interference returns true when two phases destructively cancel within tolerance.
*/
func (eng *EngineServer) Interference(a, b numeric.Phase) bool {
	sum := eng.calc.Add(a, b)

	return uint32(sum) <= 2 || uint32(sum) >= numeric.FermatPrime-2
}


