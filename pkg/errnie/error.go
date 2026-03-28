package errnie

import (
	"context"
	"errors"
)

type ErrnieError struct {
	Msg           string
	Err           error
	Op            string
	Keyvals       []any
	Reschedulable bool
	Ctx           context.Context
}

func (err *ErrnieError) Error() string {
	if err.Op != "" {
		return err.Op + ": " + err.Msg
	}

	return err.Msg
}

func Wrap(err error, keyvals ...any) *ErrnieError {
	if err == nil {
		return nil
	}
	return &ErrnieError{
		Msg:     err.Error(),
		Err:     err,
		Op:      "",
		Keyvals: keyvals,
	}
}

func (err *ErrnieError) WithContext(ctx context.Context) *ErrnieError {
	if err == nil {
		return nil
	}
	clone := *err
	clone.Ctx = ctx
	return &clone
}

func (err *ErrnieError) WithReschedule() *ErrnieError {
	if err == nil {
		return nil
	}
	clone := *err
	clone.Reschedulable = true
	return &clone
}

func (err *ErrnieError) Unwrap() error {
	return err.Err
}

func (err *ErrnieError) Is(target error) bool {
	return errors.Is(err.Err, target)
}

func (err *ErrnieError) As(target any) bool {
	return errors.As(err.Err, target)
}

func IsReschedulable(err error) bool {
	var e *ErrnieError
	if errors.As(err, &e) {
		return e.Reschedulable
	}
	return false
}

func HasContext(err error) context.Context {
	var e *ErrnieError
	if errors.As(err, &e) {
		return e.Ctx
	}
	return nil
}
