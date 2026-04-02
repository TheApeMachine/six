package cpu

import (
	"errors"

	"github.com/theapemachine/six/pkg/errnie"
)

type BackendErrorType string

const (
	ErrNilValuePointer     BackendErrorType = "nil value pointer"
	ErrInvalidInstruction  BackendErrorType = "invalid instruction"
	ErrInvalidMemoryAccess BackendErrorType = "invalid memory access"
	ErrInvalidControlFlow  BackendErrorType = "invalid control flow"
	ErrInvalidRegister     BackendErrorType = "invalid register"
)

type BackendError struct {
	*errnie.ErrnieError
	Type BackendErrorType
}

func NewBackendError(
	errType BackendErrorType, keyvals ...any,
) *BackendError {
	return &BackendError{
		ErrnieError: errnie.NewErrnieError(
			errors.New(string(errType)),
			keyvals...,
		),
		Type: errType,
	}
}
