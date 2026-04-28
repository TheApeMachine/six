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
	if err == nil || err.wrapped == nil {
		return ""
	}

	return err.wrapped.Error()
}

func (err *ErrnieError) Join(werr error) *ErrnieError {
	if err == nil || werr == nil {
		return err
	}

	err.wrapped = errors.Join(err.wrapped, werr)

	return err
}

func (err *ErrnieError) WithContext(ctx context.Context) *ErrnieError {
	if err == nil {
		return nil
	}

	err.ctx = ctx

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
	var target *ErrnieError
	if !errors.As(err, &target) || target == nil {
		return nil
	}

	return target.ctx
}
