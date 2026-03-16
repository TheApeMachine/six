package errnie

import (
	"errors"
	"io"
)

/*
Must panics if an error is not nil. Otherwise, it returns the value.

This function is useful for simplifying error handling in scenarios where errors are not expected
and should terminate the program if they occur. By panicking, it avoids the need for repetitive
error checks after every call.

Example usage:

	value := Must(someFuncThatReturnsValueAndError())
	fmt.Println(value) // This will only execute if no error occurs.
*/
func Must[T any](value T, err error) T {
	if err != nil && err != io.EOF {
		Error(err)
		panic(err)
	}

	return value
}

/*
MustVoid is used when a function returns only an error, and we need to panic if it fails.

This is helpful for simplifying functions that do not return a value but may return an error.
Instead of handling the error explicitly, MustVoid will panic if the error is non-nil.

Example usage:

	MustVoid(someFuncThatReturnsOnlyError())
*/
func MustVoid(err error) {
	if err != nil && err != io.EOF {
		Error(err)
		panic(err)
	}
}

/*
SafeMust wraps a function call, and if there is a panic, it automatically recovers.

This function is used to safely execute a function that may return an error. If an error occurs,
or if the function panics, SafeMust will recover and log the panic instead of crashing the program.
This can be useful for non-critical operations where you want the program to continue running.

The optional fallbacks parameter allows you to provide custom recovery functions that will be
executed in order if a panic occurs. Each fallback function receives the panic value as its argument,
allowing for custom error handling or cleanup operations before the default warning is logged.

Example usage:

	result := SafeMust(func() (int, error) {
		return someComputation()
	})
	fmt.Println(result)

	// With custom fallback handlers
	result := SafeMust(
		func() (int, error) {
			return someComputation()
		},
		func(p interface{}) {
			cleanup()
		},
		func(p interface{}) {
			metrics.RecordPanic(p)
		},
	)
*/
func SafeMust[T any](fn func() (T, error), fallbacks ...func(interface{})) T {
	var (
		value T
		err   error
	)

	defer func() {
		if r := recover(); r != nil {
			if len(fallbacks) != 0 {
				for _, rec := range fallbacks {
					rec(r)
				}
			}
			Warn("Recovered: %v", r)
		}
	}()

	if value, err = fn(); err != nil && err != io.EOF {
		ErrorSafe(err, false)
		panic(err)
	}

	return value
}

/*
SafeMustVoid wraps a function call that returns an error and recovers from panics.

If the function provided to SafeMustVoid returns an error or panics, this function will
recover gracefully and log the error. This is particularly useful for functions that
are expected to continue even if an error occurs, and should not crash the program.

Example usage:

	SafeMustVoid(func() error {
		return someNonCriticalOperation()
	})
*/
func SafeMustVoid(fn func() error) {
	if fn == nil {
		Error(errors.New("SafeMustVoid called with nil function"))
		panic("SafeMustVoid called with nil function")
	}

	defer func() {
		if r := recover(); r != nil {
			Log("Recovered from panic in SafeMustVoid: %v", r)
		}
	}()

	MustVoid(fn())
}
