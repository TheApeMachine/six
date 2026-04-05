package primitive

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
	return err.Error()
}

func (err *PrimitiveError) Unwrap() error {
	return err.Err
}
