package kadabra

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewKadabraError(t *testing.T) {
	t.Parallel()

	Convey("NewKadabraError carries type", t, func() {
		err := NewKadabraError(ErrKadabraRecordConflict, "key", uint64(9))

		So(err, ShouldNotBeNil)
		So(err.Type, ShouldEqual, ErrKadabraRecordConflict)
	})
}

func TestKadabraErrorError(t *testing.T) {
	t.Parallel()

	Convey("Error delegates to ErrnieError", t, func() {
		err := NewKadabraError(ErrKadabraRecordConflict)

		So(err.Error(), ShouldNotEqual, "")
	})
}
