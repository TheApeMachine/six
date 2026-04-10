package programmer

import "github.com/theapemachine/six/pkg/primitive"

/*
FinalizeNext is the continuation callback for a finalizer chain.
*/
type FinalizeNext func(*primitive.Value) ([]*primitive.Value, error)

/*
Finalizer interprets a Value after the backend has executed its program.
Implementations can inspect the Signals region and emit new derived Values.

The next callback allows multiple finalizers to compose without the compiler
needing to understand their policy.
*/
type Finalizer func(
	value *primitive.Value,
	next FinalizeNext,
) ([]*primitive.Value, error)
