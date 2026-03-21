package processor

import (
	"context"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
)

/*
TestNewInterpreterErrorOmitsDuplicateMessage verifies the typed error constructor
does not duplicate the stable Err text into Message.
*/
func TestNewInterpreterErrorOmitsDuplicateMessage(t *testing.T) {
	gc.Convey("Given a typed interpreter error", t, func() {
		interpErr := NewInterpreterError(InterpreterErrorTypeAllocationFailed)

		gc.Convey("It should leave Message empty when it matches Err", func() {
			gc.So(interpErr.Message, gc.ShouldEqual, "")
			gc.So(interpErr.Error(), gc.ShouldEqual, "interpreter error: allocation failed")
		})
	})
}

type macroRecorderStub struct {
	accept bool
	calls  int
	last   primitive.Value
}

func (stub *macroRecorderStub) StoreMacroValue(value primitive.Value) bool {
	stub.calls++
	stub.last = value

	return stub.accept
}

func TestInterpreterReifiedTraceRecording(t *testing.T) {
	gc.Convey("Given an interpreter with a macro recorder", t, func() {
		recorder := &macroRecorderStub{accept: true}
		server, err := NewInterpreterServer(
			InterpreterWithContext(context.Background()),
			InterpreterWithMacroRecorder(recorder),
		)
		gc.So(err, gc.ShouldBeNil)

		value := primitive.NeutralValue()
		value.SetProgram(primitive.OpcodeHalt, 0, 0, true)
		value.SetAffine(3, 5)

		gc.Convey("Execute should record one reified macro operator", func() {
			_, execErr := server.Execute([]primitive.Value{value})
			gc.So(execErr, gc.ShouldBeNil)
			gc.So(recorder.calls, gc.ShouldEqual, 1)
			gc.So(recorder.last.IsMacroOperator(), gc.ShouldBeTrue)
		})
	})

	gc.Convey("Given a recorder that rejects reified values", t, func() {
		recorder := &macroRecorderStub{accept: false}
		server, err := NewInterpreterServer(
			InterpreterWithContext(context.Background()),
			InterpreterWithMacroRecorder(recorder),
		)
		gc.So(err, gc.ShouldBeNil)

		value := primitive.NeutralValue()
		value.SetProgram(primitive.OpcodeHalt, 0, 0, true)
		value.SetAffine(3, 5)

		gc.Convey("Execute should return a recorder rejection error", func() {
			_, execErr := server.Execute([]primitive.Value{value})
			gc.So(execErr, gc.ShouldNotBeNil)
			gc.So(execErr.Error(), gc.ShouldContainSubstring, "macro recorder rejected")
			gc.So(recorder.calls, gc.ShouldEqual, 1)
		})
	})
}
