package errnie

type ErrnieError struct {
	Msg     string
	Err     error
	Op      string
	Keyvals []any
}

func (err *ErrnieError) Error() string {
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
	return err.Err == target
}

func (err *ErrnieError) As(target any) bool {
	return err.Err == target
}
