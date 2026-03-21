package errnie

import (
	"errors"
	"fmt"
	"io"
)

/*
Must panics if an error is not nil. Otherwise, it returns the value.
Reserved for init-time invariants where failure is unrecoverable.
Production hot paths should use Guard instead.
*/
func Must[T any](value T, err error) T {
	if err != nil && err != io.EOF {
		Error(err)
		panic(err)
	}

	return value
}

/*
MustVoid panics if err is non-nil. Reserved for init-time invariants.
*/
func MustVoid(err error) {
	if err != nil && err != io.EOF {
		Error(err)
		panic(err)
	}
}

/*
SafeMust calls fn and dispatches any returned error to the handlers
without triggering a panic. A deferred recover still catches actual
runtime faults (nil-pointer, out-of-bounds) so those do not crash
the process, but routine errors flow through the handlers as plain
data — no stack unwinding, no runtime overhead.
*/
func SafeMust[T any](fn func() (T, error), handlers ...func(error)) T {
	var zero T

	defer func() {
		if r := recover(); r != nil {
			recovered := defaultSafeGuard.recoverToError(r)

			for _, handler := range handlers {
				handler(recovered)
			}

			if len(handlers) == 0 {
				Warn("Recovered: %v", r)
			}
		}
	}()

	value, err := fn()

	if err != nil && err != io.EOF {
		for _, handler := range handlers {
			handler(err)
		}

		if len(handlers) == 0 {
			Warn("Error: %v", err)
		}

		return zero
	}

	return value
}

/*
SafeMustVoid calls fn and dispatches any returned error to the handlers
without triggering a panic. Nil fn is a programming error and panics.
*/
func SafeMustVoid(fn func() error, handlers ...func(error)) {
	if fn == nil {
		Error(errors.New("SafeMustVoid called with nil function"))
		panic("SafeMustVoid called with nil function")
	}

	defer func() {
		if r := recover(); r != nil {
			recovered := defaultSafeGuard.recoverToError(r)

			for _, handler := range handlers {
				handler(recovered)
			}

			if len(handlers) == 0 {
				Log("Recovered from panic in SafeMustVoid: %v", r)
			}
		}
	}()

	err := fn()

	if err != nil && err != io.EOF {
		for _, handler := range handlers {
			handler(err)
		}

		if len(handlers) == 0 {
			Warn("Error: %v", err)
		}
	}
}

var defaultSafeGuard SafeGuard

/*
SafeGuard encapsulates recover-and-dispatch behavior for runtime faults.
*/
type SafeGuard struct{}

/*
recoverToError converts a panic payload into an error for handler dispatch.
*/
func (guard *SafeGuard) recoverToError(r any) error {
	if err, ok := r.(error); ok {
		return err
	}

	return fmt.Errorf("%v", r)
}

/*
SafeMust2 calls fn returning two values and dispatches errors to handlers
without panic. Deferred recover catches runtime faults only.
*/
func SafeMust2[T any, U any](fn func() (T, U, error), handlers ...func(error)) (T, U) {
	var (
		v1 T
		v2 U
	)

	defer func() {
		if r := recover(); r != nil {
			recovered := defaultSafeGuard.recoverToError(r)

			for _, handler := range handlers {
				handler(recovered)
			}

			if len(handlers) == 0 {
				Warn("Recovered: %v", r)
			}
		}
	}()

	var err error
	v1, v2, err = fn()

	if err != nil && err != io.EOF {
		for _, handler := range handlers {
			handler(err)
		}

		var z1 T
		var z2 U

		return z1, z2
	}

	return v1, v2
}

/*
SafeMust3 calls fn returning three values and dispatches errors to handlers
without panic. Deferred recover catches runtime faults only.
*/
func SafeMust3[T any, U any, V any](fn func() (T, U, V, error), handlers ...func(error)) (T, U, V) {
	var (
		v1 T
		v2 U
		v3 V
	)

	defer func() {
		if r := recover(); r != nil {
			recovered := defaultSafeGuard.recoverToError(r)

			for _, handler := range handlers {
				handler(recovered)
			}

			if len(handlers) == 0 {
				Warn("Recovered: %v", r)
			}
		}
	}()

	var err error
	v1, v2, v3, err = fn()

	if err != nil && err != io.EOF {
		for _, handler := range handlers {
			handler(err)
		}

		var z1 T
		var z2 U
		var z3 V

		return z1, z2, z3
	}

	return v1, v2, v3
}
