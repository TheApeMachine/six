package vm

import (
	"errors"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewVmError(t *testing.T) {
	t.Parallel()

	Convey("NewVmError substitutes a sentinel when err is nil", t, func() {
		vmErr := NewVmError(ErrVmInvalidSequence, nil, "op")

		So(vmErr, ShouldNotBeNil)
		So(vmErr.Err, ShouldNotBeNil)
		So(vmErr.Err.Error(), ShouldContainSubstring, "no underlying error")
	})

	Convey("NewVmError preserves a concrete cause", t, func() {
		cause := errors.New("root")
		vmErr := NewVmError(ErrVmInvalidLabel, cause, "Validate")

		So(vmErr.Type, ShouldEqual, ErrVmInvalidLabel)
		So(vmErr.Op, ShouldEqual, "Validate")
		So(errors.Is(vmErr, cause), ShouldBeTrue)
		So(errors.Unwrap(vmErr), ShouldEqual, cause)
	})
}

func TestVmError_Error(t *testing.T) {
	t.Parallel()

	Convey("Given a nil VmError", t, func() {
		var vmErr *VmError

		So(vmErr.Error(), ShouldEqual, "<nil>")
	})

	Convey("Error formats type, op, and cause", t, func() {
		vmErr := NewVmError(ErrVmInvalidValue, errors.New("bad"), "Check")

		So(vmErr.Error(), ShouldContainSubstring, string(ErrVmInvalidValue))
		So(vmErr.Error(), ShouldContainSubstring, "Check")
		So(vmErr.Error(), ShouldContainSubstring, "bad")
	})
}

func TestVmError_Unwrap(t *testing.T) {
	t.Parallel()

	Convey("Unwrap on nil receiver returns nil", t, func() {
		var vmErr *VmError

		So(vmErr.Unwrap(), ShouldBeNil)
	})
}
