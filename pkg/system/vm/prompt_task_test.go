package vm

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute"
)

/*
routeCloser records whether PromptTask closed the route.
*/
type routeCloser struct {
	closed int
}

/*
Close records one route close.
*/
func (route *routeCloser) Close() error {
	route.closed++

	return nil
}

/*
TestPromptTaskCloseBeforeRead verifies PromptTask tears down backend resources
even when the worker never drains the round-trip.
*/
func TestPromptTaskCloseBeforeRead(t *testing.T) {
	gc.Convey("Given a prompt task closed before Read", t, func() {
		backend, err := compute.NewBackend(
			compute.BackendWithOperations(),
		)
		gc.So(err, gc.ShouldBeNil)

		route := &routeCloser{}
		task := NewPromptTask([]byte("prompt"), backend, route)

		gc.Convey("It should close both backend and route", func() {
			gc.So(task.Close(), gc.ShouldBeNil)
			gc.So(route.closed, gc.ShouldEqual, 1)

			_, writeErr := backend.Write([]byte("x"))
			gc.So(writeErr, gc.ShouldNotBeNil)
		})
	})
}
