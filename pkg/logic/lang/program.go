package lang

import (
	"bytes"
	"errors"
	"fmt"
	"io"

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
		if err = program.refreshBuffer(); err != nil {
			return 0, err
		}
	}

	buffer, err := program.Buffer()
	if err != nil {
		return 0, err
	}

	if len(buffer) == 0 {
		return 0, io.EOF
	}

	n = copy(p, buffer)
	err = program.SetBuffer(buffer[n:])
	if err != nil {
		return 0, err
	}

	return n, nil
}

/*
Write appends an incoming Program snapshot onto the receiver's value list.
*/
func (program *Program) Write(p []byte) (n int, err error) {
	decoder := capnp.NewDecoder(bytes.NewReader(p))
	msg, err := decoder.Decode()
	if err != nil {
		return 0, err
	}

	incoming, err := ReadRootProgram(msg)
	if err != nil {
		return 0, err
	}

	incomingValues, err := incoming.Values()
	if err != nil {
		return 0, err
	}

	incomingLen := incomingValues.Len()
	if incomingLen == 0 {
		return len(p), nil
	}

	currentLen := 0
	var currentValues primitive.Value_List

	if program.HasValues() {
		currentValues, err = program.Values()
		if err != nil {
			return 0, err
		}

		currentLen = currentValues.Len()
	}

	merged, err := primitive.NewValue_List(
		program.Segment(),
		int32(currentLen+incomingLen),
	)
	if err != nil {
		return 0, err
	}

	for index := 0; index < currentLen; index++ {
		destination := merged.At(index)
		destination.CopyFrom(currentValues.At(index))
	}

	for index := 0; index < incomingLen; index++ {
		destination := merged.At(currentLen + index)
		destination.CopyFrom(incomingValues.At(index))
	}

	if err = program.SetValues(merged); err != nil {
		return 0, err
	}

	if err = program.refreshBuffer(); err != nil {
		return 0, err
	}

	return len(p), nil
}

/*
refreshBuffer snapshots the current Program into the transient read buffer.
*/
func (program *Program) refreshBuffer() error {
	var buffer bytes.Buffer

	msg, err := program.snapshotMessage()
	if err != nil {
		return err
	}

	if err := capnp.NewEncoder(&buffer).Encode(msg); err != nil {
		return err
	}

	return program.SetBuffer(buffer.Bytes())
}

/*
snapshotMessage lifts the current Program into a fresh root message for io transport.
*/
func (program *Program) snapshotMessage() (*capnp.Message, error) {
	msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		return nil, err
	}

	root, err := NewRootProgram(seg)
	if err != nil {
		return nil, err
	}

	if program.HasValues() {
		values, err := program.Values()
		if err != nil {
			return nil, err
		}

		snapshot, err := primitive.NewValue_List(seg, int32(values.Len()))
		if err != nil {
			return nil, err
		}

		for index := 0; index < values.Len(); index++ {
			destination := snapshot.At(index)
			destination.CopyFrom(values.At(index))
		}

		if err := root.SetValues(snapshot); err != nil {
			return nil, err
		}
	}

	return msg, nil
}

/*
Close releases Program io state.
*/
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
	return &ProgramError{Message: string(err), Err: errors.New(string(err))}
}

func (err *ProgramError) Error() string {
	return fmt.Errorf("program error: %s: %w", err.Message, err.Err).Error()
}
