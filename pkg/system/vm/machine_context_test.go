package vm

import (
	"context"
	"io"
	"testing"
	"time"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/system/cluster"
	"github.com/theapemachine/six/pkg/system/pool"
)

/*
TestNewMachineWithoutContextDoesNotPanic verifies that NewMachine initializes
its own context and cancel function if they are not provided via options,
preventing nil pointer dereferences.
*/
func TestNewMachineWithoutContextDoesNotPanic(t *testing.T) {
	gc.Convey("Given no MachineWithContext", t, func() {
		var machine *Machine

		gc.Convey("When creating a new Machine", func() {
			gc.So(func() {
				machine = NewMachine()
			}, gc.ShouldNotPanic)

			gc.Convey("Then it should not be nil", func() {
				gc.So(machine, gc.ShouldNotBeNil)
			})

			gc.Convey("And it should have an initialized context and cancel", func() {
				gc.So(machine.ctx, gc.ShouldNotBeNil)
				gc.So(machine.cancel, gc.ShouldNotBeNil)
			})

			if machine != nil {
				machine.Close()
			}
		})
	})
}

/*
TestMachinePromptReturnsBackendErrors verifies Prompt surfaces backend failures
instead of reporting an empty success.
*/
func TestMachinePromptReturnsBackendErrors(t *testing.T) {
	gc.Convey("Given a machine whose router has no cantilever service", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		machine := NewMachine(
			MachineWithContext(ctx),
		)
		defer machine.Close()

		oldRouter := machine.booter.router
		machine.booter.router = cluster.NewRouter(cluster.RouterWithContext(machine.ctx))
		defer oldRouter.Close()

		gc.Convey("Prompt should return the backend error", func() {
			result, err := machine.Prompt("Roy is in the ")
			gc.So(result, gc.ShouldBeNil)
			gc.So(err, gc.ShouldNotBeNil)
			gc.So(err.Error(), gc.ShouldContainSubstring, "no service registered")
		})
	})
}

/*
TestMachineAwaitPromptResultRejectsNilValue verifies the worker result store
cannot report a missing prompt payload as a successful completion.
*/
func TestMachineAwaitPromptResultRejectsNilValue(t *testing.T) {
	gc.Convey("Given a scheduled prompt job with no output bytes", t, func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		machine := NewMachine(
			MachineWithContext(ctx),
		)
		defer machine.Close()

		jobID := "machine/prompt/nil-result"
		err := machine.workerPool.Schedule(
			jobID,
			pool.COMPUTE,
			&emptyPromptTask{},
			pool.WithContext(ctx),
		)
		gc.So(err, gc.ShouldBeNil)

		gc.Convey("awaitPromptResult should return an explicit nil-result error", func() {
			result, err := machine.awaitPromptResult(ctx, jobID)
			gc.So(result, gc.ShouldBeNil)
			gc.So(err, gc.ShouldNotBeNil)
			gc.So(err.Error(), gc.ShouldContainSubstring, "nil prompt result")
		})
	})
}

/*
emptyPromptTask simulates a completed job that produced no payload.
*/
type emptyPromptTask struct{}

/*
Read returns EOF immediately so the worker stores a nil result value.
*/
func (task *emptyPromptTask) Read(_ []byte) (n int, err error) {
	return 0, io.EOF
}

/*
Write accepts the worker write-back path.
*/
func (task *emptyPromptTask) Write(p []byte) (n int, err error) {
	return len(p), nil
}

/*
Close completes the task teardown.
*/
func (task *emptyPromptTask) Close() error {
	return nil
}
