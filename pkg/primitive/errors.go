package primitive

import "fmt"

type PrimitiveErrorType string

const (
	ErrPrimitiveInvalidValue PrimitiveErrorType = "invalid value"
)

type PrimitiveError struct {
	Type PrimitiveErrorType
	Err  error
	Op   string
}

func NewPrimitiveError(
	typ PrimitiveErrorType,
	err error,
	op string,
) *PrimitiveError {
	return &PrimitiveError{
		Type: typ,
		Err:  err,
		Op:   op,
	}
}

func (err *PrimitiveError) Error() string {
	if err == nil {
		return "<nil PrimitiveError>"
	}

	if err.Err != nil {
		return fmt.Sprintf("primitive %s (%s): %v", err.Op, err.Type, err.Err)
	}

	return fmt.Sprintf("primitive %s (%s)", err.Op, err.Type)
}

func (err *PrimitiveError) Unwrap() error {
	return err.Err
}

