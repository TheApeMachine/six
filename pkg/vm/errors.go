package vm

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
	return &VmError{Type: typ, Err: err, Op: op}
}

func (err *VmError) Error() string {
	return err.Err.Error()
}

func (err *VmError) Unwrap() error {
	return err.Err
}
