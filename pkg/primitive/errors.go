package primitive

import "errors"

type ValueErrorType string

const (
	ValueErrorFailedToken          ValueErrorType = "failed_token"
	ValueErrorDivergence           ValueErrorType = "divergence"
	ValueErrorDataFull             ValueErrorType = "data_full"
	ValueErrorInvalidProgramWord   ValueErrorType = "invalid_program_word"
	ValueErrorRefcountUnderflow    ValueErrorType = "refcount_underflow"
	ValueErrorFailedByteConversion ValueErrorType = "failed_byte_conversion"
	ValueErrorNotTombstoned        ValueErrorType = "not_tombstoned"
)

type ValueError struct {
	Err error
}

func NewValueError(err ValueErrorType) *ValueError {
	return &ValueError{Err: errors.New(string(err))}
}

func (e *ValueError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return "value error"
}

func (e *ValueError) Unwrap() error {
	return e.Err
}
