package kadabra

import (
	"errors"

	"github.com/theapemachine/six/pkg/errnie"
)

type KadabraErrorType string

const (
	ErrKadabraRecordConflict KadabraErrorType = "record conflict"
)

type KadabraError struct {
	*errnie.ErrnieError
	Type KadabraErrorType
}

func NewKadabraError(
	errType KadabraErrorType, keyvals ...any,
) *KadabraError {
	return &KadabraError{
		Type: errType,
		ErrnieError: errnie.NewErrnieError(
			errors.New(string(errType)),
			keyvals...,
		),
	}
}

func (err *KadabraError) Error() string {
	return err.ErrnieError.Error()
}
