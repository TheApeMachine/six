package processor

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
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
