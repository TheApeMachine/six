package network

import (
	"errors"
	"fmt"

	"github.com/theapemachine/six/pkg/errnie"
)

type NetworkErrorType string

const (
	ErrTransportFailure NetworkErrorType = "transport failure"
)

type NetworkError struct {
	*errnie.ErrnieError
	Type NetworkErrorType
}

func NewNetworkError(
	errType NetworkErrorType, keyvals ...any,
) *NetworkError {
	return &NetworkError{
		ErrnieError: errnie.NewErrnieError(
			errors.New(string(errType)),
			keyvals...,
		),
		Type: errType,
	}
}

// TransportError is a structured error for transport-layer failures.
type TransportError struct {
	Layer string
	Op    string
	Err   error
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("%s %s: %v", e.Layer, e.Op, e.Err)
}

func (e *TransportError) Unwrap() error { return e.Err }
