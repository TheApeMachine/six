package lang

import (
	"bytes"
	"fmt"

	capnp "capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
)

type programOpts func(*Program)

func New(opts ...programOpts) (Program, error) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))

	if err != nil {
		return Program{}, errnie.Error(
			NewProgramError(ProgramErrorTypeAllocationFailed),
		)
	}

	program, err := NewProgram(seg)

	if err != nil {
		return Program{}, errnie.Error(
			NewProgramError(ProgramErrorTypeAllocationFailed),
		)
	}

	return program, nil
}

func (program *Program) Handle() []byte {
	// Do something with the data...
	values, err := program.Values()

	if err != nil {
		return nil
	}

	return values.EncodeAsPtr(program.Segment()).Data()
}

/*
Read Marshals the Cap 'n Proto object into a byte slice.
*/
func (program *Program) Read(p []byte) (n int, err error) {
	if !program.HasBuffer() {
		program.SetBuffer(program.EncodeAsPtr(program.Segment()).Data())
	}

	if buf, err := program.Buffer(); err != nil || len(buf) == 0 {
		program.SetBuffer(program.EncodeAsPtr(program.Segment()).Data())
	}

	buffer, err := program.Buffer()
	if err != nil {
		return 0, err
	}

	n = copy(p, buffer)
	program.SetBuffer(buffer[n:])

	return n, nil
}

func (program *Program) Write(p []byte) (n int, err error) {
	if !program.HasBuffer() {
		program.SetBuffer(program.EncodeAsPtr(program.Segment()).Data())
	}

	buffer, err := program.Buffer()
	if err != nil {
		decoder := capnp.NewDecoder(bytes.NewReader(p))
		msg, err := decoder.Decode()
		if err != nil {
			return 0, err
		}

		value, err := primitive.ReadRootValue(msg)
		if err != nil {
			return 0, err
		}

		values, err := program.Values()
		if err != nil {
			return 0, err
		}

		values.Set(values.Len(), value)

		return 0, err
	}

	n = copy(buffer, p)
	program.SetBuffer(buffer[n:])

	return n, nil
}

func (program *Program) Close() error {
	return nil
}

type ProgramError struct {
	Message string
	Err     error
}

type ProgramErrorType string

const (
	ProgramErrorTypeAllocationFailed    ProgramErrorType = "allocation failed"
	ProgramErrorTypeStartAndTargetEmpty ProgramErrorType = "start and target values cannot be empty"
	ProgramErrorTypeSeedPairRequired    ProgramErrorType = "write requires at least start and target seeds"
	ProgramErrorTypeCandidatePoolEmpty  ProgramErrorType = "candidate pool cannot be empty"
	ProgramErrorTypeProgramStalled      ProgramErrorType = "program stalled"
	ProgramErrorTypeExecutionStalled    ProgramErrorType = "execution stalled"
	ProgramErrorTypeProgramExhausted    ProgramErrorType = "program exhausted"
)

func NewProgramError(err ProgramErrorType) *ProgramError {
	return &ProgramError{Message: string(err), Err: fmt.Errorf(string(err))}
}

func (err *ProgramError) Error() string {
	return fmt.Errorf("program error: %s: %w", err.Message, err.Err).Error()
}
