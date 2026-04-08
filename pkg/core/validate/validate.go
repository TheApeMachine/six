package validate

import (
	"errors"
	"reflect"
	"slices"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

var (
	/*
		ErrValueNil signals RequireChainLinkage received a nil pointer.
	*/
	ErrValueNil = errors.New("validate: primitive.Value is nil")

	/*
		ErrValueIDUnset signals ID word is zero; such a Value cannot
		participate in an identified chain.
	*/
	ErrValueIDUnset = errors.New("validate: primitive.Value ID is unset")

	/*
		ErrValueChainUnlinked signals both Prev and Next words are zero.
		Consumers that only need an ID and affinity should not call
		RequireChainLinkage; streaming ingest may leave both zero until a
		late linker runs.
	*/
	ErrValueChainUnlinked = errors.New("validate: primitive.Value has neither Prev nor Next link")
)

/*
missingDependency reports whether a value passed as a required dependency
is absent. A nil interface value is absent. So is a typed nil pointer, map,
slice, channel, or func stored in an any slot — the usual Go interface nil
trap — because those values cannot be used safely without checking Kind and
IsNil first.
*/
func missingDependency(obj any) bool {
	if obj == nil {
		return true
	}

	value := reflect.ValueOf(obj)

	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return value.IsNil()
	default:
		return false
	}
}

/*
Require validates that all required dependencies are present (see
missingDependency). Keys are checked in sorted order so the reported name
is stable when several entries are wrong.

Pass a map of name → value; if any dependency is missing, returns an error
with a clear message (e.g. "pool is required"). Use in constructors after
options are applied so callers fail fast instead of ad-hoc nil checks
throughout Run() and other methods.
*/
func Require(objs map[string]any) error {
	names := make([]string, 0, len(objs))

	for name := range objs {
		names = append(names, name)
	}

	slices.Sort(names)

	for _, name := range names {
		if missingDependency(objs[name]) {
			return errors.New(name + " is required")
		}
	}

	return nil
}

/*
RequireChainLinkage is for subsystems that actually walk sequence edges:
they must reject Values that could not possibly sit in a doubly-linked
stream. Rules: non-nil pointer, non-zero ID, and at least one of the
configured Prev or Next regions non-zero.

Hot-path loaders and parallel GPU stages remain free to emit or pass
Values that violate this; only the boundary that assumes linkage calls
here (or returns a typed error upward).
*/
func RequireChainLinkage(value *primitive.Value) error {
	if value == nil {
		return ErrValueNil
	}

	if value.ID() == 0 {
		return ErrValueIDUnset
	}

	prevWord := (*value)[core.Cfg.Value.Region.Prev.Start]
	nextWord := (*value)[core.Cfg.Value.Region.Next.Start]

	if prevWord == 0 && nextWord == 0 {
		return ErrValueChainUnlinked
	}

	return nil
}
