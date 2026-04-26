package ts

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestLanguage(t *testing.T) {
	t.Parallel()

	Convey("Language exposes a non-nil sitter language", t, func() {
		language := Language()
		So(language, ShouldNotBeNil)
		So(language.SymbolCount(), ShouldBeGreaterThan, 0)
	})
}

func TestParse(t *testing.T) {
	t.Parallel()

	Convey("Given feed source with pipes and bonds", t, func() {
		source := []byte(`
[ { a } ] <= [ { b } ]
; trailing
`)
		tree, err := Parse(context.Background(), source)
		So(err, ShouldBeNil)
		So(tree, ShouldNotBeNil)

		root := tree.RootNode()
		So(root.IsNull(), ShouldBeFalse)
		So(root.HasError(), ShouldBeFalse)
		So(root.Type(), ShouldEqual, "source_file")
	})

	Convey("Given compact pipe body", t, func() {
		source := []byte(`[ ( x y ) ] <= [ { z } ]`)
		tree, err := Parse(context.Background(), source)
		So(err, ShouldBeNil)
		So(tree.RootNode().HasError(), ShouldBeFalse)
	})

	Convey("Given emit wrapper", t, func() {
		source := []byte(`<[ { a } ]> [ { b } ]`)
		tree, err := Parse(context.Background(), source)
		So(err, ShouldBeNil)
		So(tree.RootNode().HasError(), ShouldBeFalse)
	})
}

func BenchmarkParse(b *testing.B) {
	source := []byte(`
<[
  { B(prev) B(id) ^ }
  { B(next) B(id) ^ }
] <= [
  { B(tokens) B(signals[0,1]) ^ }
]>
`)
	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		tree, err := Parse(context.Background(), source)
		if err != nil || tree.RootNode().HasError() {
			b.Fatal(err)
		}
	}
}
