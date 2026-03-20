package substrate

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestGraphServer(t *testing.T) {
	Convey("GraphServer", t, func() {
		graph := NewGraphServer()
		So(graph, ShouldNotBeNil)
	})
}

func TestWrite(t *testing.T) {
	Convey("Write", t, func() {
		graph := NewGraphServer()
		So(graph, ShouldNotBeNil)
	})
}

func TestPrompt(t *testing.T) {
	Convey("Prompt", t, func() {
		graph := NewGraphServer()
		So(graph, ShouldNotBeNil)
	})
}

func TestDone(t *testing.T) {
	Convey("Done", t, func() {
		graph := NewGraphServer()
		So(graph, ShouldNotBeNil)
	})
}

func TestRecursiveFold(t *testing.T) {
	Convey("RecursiveFold", t, func() {
		graph := NewGraphServer()
		So(graph, ShouldNotBeNil)
	})
}
