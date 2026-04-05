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
	Layer    string
	Op       string
	Mode     TransportFailureMode
	Systemic bool
	Err      error
}

func (e *TransportError) Error() string {
	if e.Mode == "" || e.Mode == TransportFailureNone {
		return fmt.Sprintf("%s %s: %v", e.Layer, e.Op, e.Err)
	}

	return fmt.Sprintf("%s %s (%s): %v", e.Layer, e.Op, e.Mode, e.Err)
}

func (e *TransportError) Unwrap() error { return e.Err }

// TransportStatusError is returned by monitor and breaker state transitions.
type TransportStatusError string

const (
	ErrTransportCircuitOpen TransportStatusError = "transport: circuit open"
)

// Error implements the error interface for TransportStatusError.
func (transportErr TransportStatusError) Error() string {
	return string(transportErr)
}
