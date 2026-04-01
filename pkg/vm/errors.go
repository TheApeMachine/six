package vm

import (
	"errors"

	"github.com/theapemachine/six/pkg/errnie"
)

type MachineErrorType string

const (
	ErrNoContext        MachineErrorType = "no context provided"
	ErrNotValidated     MachineErrorType = "machine configuration did not pass validation"
	ErrDatasetNotClosed MachineErrorType = "dataset was not closed properly"
	ErrValueError       MachineErrorType = "error constructing Value from input"
	ErrStreamFailed     MachineErrorType = "stream failed"
)

type MachineError struct {
	*errnie.ErrnieError
	Type MachineErrorType
}

func NewMachineError(
	errType MachineErrorType, keyvals ...any,
) *MachineError {
	return &MachineError{
		ErrnieError: errnie.NewErrnieError(
			errors.New(string(errType)),
			keyvals...,
		),
		Type: errType,
	}
}
