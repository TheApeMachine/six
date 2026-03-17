package errnie

import (
	"errors"
	"fmt"
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

The optional handlers parameter accepts error handler functions that receive the actual typed
error when a panic or error occurs. Each handler is called in order, allowing for contextual
error logging, metrics, or structured cleanup. Define a per-file handler alongside your typed
errors to give every SafeMust call site proper error context.

Example usage:

	result := SafeMust(func() (int, error) {
		return someComputation()
	})
	fmt.Println(result)

	// With per-file error handler
	result := SafeMust(
		func() (int, error) {
			return someComputation()
		},
		handleMyPackageError,
	)
*/
func SafeMust[T any](fn func() (T, error), handlers ...func(error)) T {
	var (
		value T
		err   error
	)

	defer func() {
		if r := recover(); r != nil {
			recovered := recoverToError(r)

			for _, handler := range handlers {
				handler(recovered)
			}

			if len(handlers) == 0 {
				Warn("Recovered: %v", r)
			}
		}
	}()

	if value, err = fn(); err != nil && err != io.EOF {
		for _, handler := range handlers {
			handler(err)
		}

		if len(handlers) == 0 {
			ErrorSafe(err, false)
		}

		panic(err)
	}

	return value
}

/*
SafeMustVoid wraps a function call that returns an error and recovers from panics.

If the function provided to SafeMustVoid returns an error or panics, this function will
recover gracefully and log the error. Accepts optional error handlers identical to SafeMust.

Example usage:

	SafeMustVoid(func() error {
		return someNonCriticalOperation()
	}, handleMyPackageError)
*/
func SafeMustVoid(fn func() error, handlers ...func(error)) {
	if fn == nil {
		Error(errors.New("SafeMustVoid called with nil function"))
		panic("SafeMustVoid called with nil function")
	}

	defer func() {
		if r := recover(); r != nil {
			recovered := recoverToError(r)

			for _, handler := range handlers {
				handler(recovered)
			}

			if len(handlers) == 0 {
				Log("Recovered from panic in SafeMustVoid: %v", r)
			}
		}
	}()

	MustVoid(fn())
}

/*
SafeMust2 wraps a function call returning two values and an error.
*/
func SafeMust2[T any, U any](fn func() (T, U, error), handlers ...func(error)) (T, U) {
	var (
		v1  T
		v2  U
		err error
	)

	defer func() {
		if r := recover(); r != nil {
			recovered := recoverToError(r)

			for _, handler := range handlers {
				handler(recovered)
			}

			if len(handlers) == 0 {
				Warn("Recovered: %v", r)
			}
		}
	}()

	if v1, v2, err = fn(); err != nil && err != io.EOF {
		for _, handler := range handlers {
			handler(err)
		}

		if len(handlers) == 0 {
			ErrorSafe(err, false)
		}

		panic(err)
	}

	return v1, v2
}

/*
SafeMust3 wraps a function call returning three values and an error.
*/
func SafeMust3[T any, U any, V any](fn func() (T, U, V, error), handlers ...func(error)) (T, U, V) {
	var (
		v1  T
		v2  U
		v3  V
		err error
	)

	defer func() {
		if r := recover(); r != nil {
			recovered := recoverToError(r)

			for _, handler := range handlers {
				handler(recovered)
			}

			if len(handlers) == 0 {
				Warn("Recovered: %v", r)
			}
		}
	}()

	if v1, v2, v3, err = fn(); err != nil && err != io.EOF {
		for _, handler := range handlers {
			handler(err)
		}

		if len(handlers) == 0 {
			ErrorSafe(err, false)
		}

		panic(err)
	}

	return v1, v2, v3
}

/*
recoverToError converts a recover() value into a proper error.
*/
func recoverToError(r any) error {
	if err, ok := r.(error); ok {
		return err
	}

	return fmt.Errorf("%v", r)
}
