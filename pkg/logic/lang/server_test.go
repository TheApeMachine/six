package lang

import (
	"context"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/store/data"
)

/*
valueBlocksMatch verifies that two Values carry identical raw state.
*/
func valueBlocksMatch(left data.Value, right data.Value) bool {
	for index := range 8 {
		if left.Block(index) != right.Block(index) {
			return false
		}
	}

	return true
}

/*
TestProgramServerWrite streams seed Values through the RPC surface and verifies
the ProgramServer retains the exact native program cells after Done.
*/
func TestProgramServerWrite(t *testing.T) {
	gc.Convey("Given a ProgramServer and streamed native Value seeds", t, func() {
		server := NewProgramServer(
			ProgramServerWithContext(context.Background()),
		)
		defer server.Close()

		client := server.Client("logic/lang/server_test")
		seeds := []data.Value{
			data.BaseValue('A'),
			data.BaseValue('B'),
			data.NeutralValue(),
		}

		gc.Convey("Write should retain the exact program values after Done", func() {
			err := client.Write(context.Background(), func(params Evaluator_write_Params) error {
				list, err := data.ValueSliceToList(seeds)
				if err != nil {
					return err
				}

				return params.SetSeed(list)
			})

			gc.So(err, gc.ShouldBeNil)

			future, release := client.Done(context.Background(), nil)
			defer release()

			_, err = future.Struct()
			gc.So(err, gc.ShouldBeNil)
			gc.So(len(server.values), gc.ShouldEqual, len(seeds))

			for index := range seeds {
				gc.So(valueBlocksMatch(server.values[index], seeds[index]), gc.ShouldBeTrue)
			}
		})
	})
}

/*
TestProgramServerLifecycle verifies the local client wiring and Close teardown.
*/
func TestProgramServerLifecycle(t *testing.T) {
	gc.Convey("Given a ProgramServer with a local RPC client", t, func() {
		server := NewProgramServer(
			ProgramServerWithContext(context.Background()),
		)

		client := server.Client("logic/lang/lifecycle")

		gc.Convey("Client should be valid and Close should release pipe resources", func() {
			gc.So(client.IsValid(), gc.ShouldBeTrue)
			gc.So(server.serverConn, gc.ShouldNotBeNil)
			gc.So(server.clientConns["logic/lang/lifecycle"], gc.ShouldNotBeNil)

			err := server.Close()

			gc.So(err, gc.ShouldBeNil)
			gc.So(server.serverConn, gc.ShouldBeNil)
			gc.So(server.serverSide, gc.ShouldBeNil)
			gc.So(server.clientSide, gc.ShouldBeNil)
			gc.So(len(server.clientConns), gc.ShouldEqual, 0)
		})
	})
}
