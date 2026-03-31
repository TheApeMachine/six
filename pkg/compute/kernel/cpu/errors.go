package cpu

import (
	"errors"

	"github.com/theapemachine/six/pkg/errnie"
)

type SimdeezNutsErrorType string

const (
	ErrNilValuePointer     SimdeezNutsErrorType = "nil value pointer"
	ErrInvalidInstruction  SimdeezNutsErrorType = "invalid instruction"
	ErrInvalidMemoryAccess SimdeezNutsErrorType = "invalid memory access"
	ErrInvalidControlFlow  SimdeezNutsErrorType = "invalid control flow"
	ErrInvalidRegister     SimdeezNutsErrorType = "invalid register"
)

type SimdeezNutsError struct {
	*errnie.ErrnieError
	Type SimdeezNutsErrorType
}

func NewSimdeezNutsError(
	errType SimdeezNutsErrorType, keyvals ...any,
) *SimdeezNutsError {
	return &SimdeezNutsError{
		ErrnieError: errnie.NewErrnieError(
			errors.New(string(errType)),
			keyvals...,
		),
		Type: errType,
	}
}
