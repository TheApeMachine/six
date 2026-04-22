package errnie

import (
	"context"
	"errors"
)

type ErrnieErrorType string

type ErrnieError struct {
	ctx           context.Context
	wrapped       error
	keyvals       []any
	reschedulable bool
}

func NewErrnieError(err error, keyvals ...any) *ErrnieError {
	if err == nil {
		return nil
	}

	return &ErrnieError{
		wrapped: err,
		keyvals: keyvals,
	}
}

func (err *ErrnieError) Error() string {
	return err.wrapped.Error()
}

func (err *ErrnieError) Join(werr error) *ErrnieError {
	errors.Join(err.wrapped, werr)
	return err
}

func (err *ErrnieError) Unwrap() error {
	if err == nil {
		return nil
	}

	return err.wrapped
}

func IsReschedulable(err error) bool {
	if e, ok := err.(*ErrnieError); ok {
		return e.reschedulable
	}
	return false
}

func HasContext(err error) context.Context {
	return nil // placeholder, not implemented cleanly in this branch yet
}

