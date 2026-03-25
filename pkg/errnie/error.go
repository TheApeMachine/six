package errnie

import "errors"

type ErrnieError struct {
	Msg     string
	Err     error
	Op      string
	Keyvals []any
}

func (err *ErrnieError) Error() string {
	if err.Op != "" {
		return err.Op + ": " + err.Msg
	}

	return err.Msg
}

func Wrap(err error, keyvals ...any) error {
	return &ErrnieError{
		Msg:     err.Error(),
		Err:     err,
		Op:      "",
		Keyvals: keyvals,
	}
}

func (err *ErrnieError) Unwrap() error {
	return err.Err
}

func (err *ErrnieError) Is(target error) bool {
	return errors.Is(err.Err, target)
}

func (err *ErrnieError) As(target any) bool {
	return errors.As(err.Err, target)
}
