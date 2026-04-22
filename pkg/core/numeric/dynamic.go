package numeric

import (
	"sync"

	"github.com/theapemachine/six/pkg/core/numeric/adaptive"
	"github.com/theapemachine/six/pkg/errnie"
)

/*
Dynamic is a reactive numeric primitive. It takes
the output of the previous step in the chain and
any number of raw observations, and returns a
derived value. Dynamics are stateful — they learn
from what they see.
*/
type Dynamic interface {
	Next(float64, ...float64) (float64, error)
	Reset() error
}

/*
Derived chains multiple Dynamic values together.
Each dynamic receives the output of the previous
one, forming a causal pipeline: raw signal flows
in, derived value flows out. No constants, no
configuration — every number comes from the data
or from the dynamic below.
*/
type Derived struct {
	mu        sync.RWMutex
	dynamics  []Dynamic
	lastValue float64
}

type DerivedOption func(*Derived)

/*
NewDerived creates a new Derived with the given
dynamics. The dynamics are applied in order, each
feeding into the next.
*/
func NewDerived(opts ...DerivedOption) *Derived {
	derived := &Derived{}

	for _, opt := range opts {
		opt(derived)
	}

	return derived
}

/*
Next pushes the given observations through the
chain of dynamics and returns the final derived
value. Each dynamic receives the previous output
as its first argument, allowing the chain to
build causally.
*/
func (derived *Derived) Next(values ...float64) (float64, error) {
	var out float64

	for _, dynamic := range derived.dynamics {
		result, err := dynamic.Next(out, values...)
		if err != nil {
			return 0, errnie.Error(err)
		}

		out = result
	}

	derived.mu.Lock()
	derived.lastValue = out
	derived.mu.Unlock()

	return out, nil
}

/*
Reset clears all dynamics in the chain back to
their initial states.
*/
func (derived *Derived) Reset() error {
	for _, dynamic := range derived.dynamics {
		if err := dynamic.Reset(); err != nil {
			return errnie.Error(err)
		}
	}

	return nil
}

/*
Value returns the last output of the chain without
pushing a new observation.
*/
func (derived *Derived) Value() float64 {
	derived.mu.RLock()
	defer derived.mu.RUnlock()

	return derived.lastValue
}

/*
Populated reports whether the chain has dynamics attached. Empty shells
inserted into maps still marshal as noisy {}; JSON export uses this to
omit unused signal slots.
*/
func (derived *Derived) Populated() bool {
	if derived == nil {
		return false
	}

	derived.mu.RLock()
	n := len(derived.dynamics)
	derived.mu.RUnlock()

	return n > 0
}

/*
Clone returns a deep copy of the dynamic chain and lastValue so mutations
via Next on the clone do not affect the source. Supported concrete
dynamics are cloned explicitly; unknown types are shared as a last resort.
*/
func (derived *Derived) Clone() *Derived {
	if derived == nil {
		return nil
	}

	derived.mu.RLock()
	last := derived.lastValue
	src := derived.dynamics
	derived.mu.RUnlock()

	out := make([]Dynamic, len(src))

	for idx, d := range src {
		out[idx] = cloneDynamic(d)
	}

	return &Derived{
		dynamics:  out,
		lastValue: last,
	}
}

func cloneDynamic(d Dynamic) Dynamic {
	if d == nil {
		return nil
	}

	switch x := d.(type) {
	case *adaptive.EMA:
		return x.Clone()
	case *adaptive.SigmaClamp:
		return x.Clone()
	default:
		return d
	}
}

/*
NewDerivedFrom creates a Derived pre-seeded with a constant value
and no dynamics chain. Used to pass snapshot values through the
Prediction signal map without building a full adaptive pipeline.
*/
func NewDerivedFrom(value float64) *Derived {
	return &Derived{lastValue: value}
}

/*
WithDynamics sets the dynamics for the derived chain.
*/
func WithDynamics(dynamics ...Dynamic) DerivedOption {
	return func(derived *Derived) {
		derived.dynamics = dynamics
	}
}

