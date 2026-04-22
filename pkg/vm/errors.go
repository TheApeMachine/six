package vm

import (
	"errors"
	"fmt"
)

type VmErrorType string

const (
	ErrVmInvalidSequence VmErrorType = "invalid sequence"
	ErrVmInvalidLabel    VmErrorType = "invalid label"
	ErrVmInvalidValue    VmErrorType = "invalid value"
)

type VmError struct {
	Type VmErrorType
	Err  error
	Op   string
}

func NewVmError(typ VmErrorType, err error, op string) *VmError {
	if err == nil {
		err = errors.New("vm: no underlying error")
	}

	return &VmError{Type: typ, Err: err, Op: op}
}

func (err *VmError) Error() string {
	if err == nil {
		return "<nil>"
	}

	underlying := err.Err

	if underlying == nil {
		underlying = errors.New("unknown")
	}

	return fmt.Sprintf("%s %s: %v", err.Type, err.Op, underlying)
}

func (err *VmError) Unwrap() error {
	if err == nil {
		return nil
	}

	return err.Err
}

