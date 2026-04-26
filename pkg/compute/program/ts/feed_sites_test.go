package ts

import (
	"context"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestFeedSites(t *testing.T) {
	t.Parallel()

	Convey("Given pipes and bonds in order", t, func() {
		src := []byte(`[ { a } ] <= [ { b } ]`)
		sites, err := FeedSites(context.Background(), src)
		So(err, ShouldBeNil)
		So(len(sites), ShouldEqual, 2)
		So(sites[0].Emit, ShouldBeFalse)
		So(sites[0].Body, ShouldEqual, `{ a }`)
		So(sites[1].Emit, ShouldBeFalse)
		So(sites[1].Body, ShouldEqual, `{ b }`)
	})

	Convey("Given emit wrapper", t, func() {
		src := []byte(`<[ { x } ]> [ { y } ]`)
		sites, err := FeedSites(context.Background(), src)
		So(err, ShouldBeNil)
		So(len(sites), ShouldEqual, 2)
		So(sites[0].Emit, ShouldBeTrue)
		So(sites[0].Body, ShouldEqual, `{ x }`)
		So(sites[1].Emit, ShouldBeFalse)
		So(sites[1].Body, ShouldEqual, `{ y }`)
	})

	Convey("Given emit wrapper without closing angle", t, func() {
		src := []byte(`<[ { x } ] [ { y } ]`)
		sites, err := FeedSites(context.Background(), src)
		So(err, ShouldBeNil)
		So(len(sites), ShouldEqual, 2)
		So(sites[0].Emit, ShouldBeTrue)
		So(sites[1].Emit, ShouldBeTrue)
	})

	Convey("Given an indexed operand with internal whitespace", t, func() {
		src := []byte(`[ { B(tokens) B(signals[0, 1]) ^ } ]`)
		sites, err := FeedSites(context.Background(), src)
		So(err, ShouldBeNil)
		So(len(sites), ShouldEqual, 1)
		So(sites[0].Body, ShouldEqual, `{ B(tokens) B(signals[0, 1]) ^ }`)
	})

	Convey("Given invalid feed source", t, func() {
		src := []byte(`[ { B(tokens) B(signals[0, 1]) ^ ]`)
		_, err := FeedSites(context.Background(), src)
		So(err, ShouldNotBeNil)
		So(strings.Contains(err.Error(), "syntax error in feed source at"), ShouldBeTrue)
	})
}

func TestParseFeedProgram(t *testing.T) {
	t.Parallel()

	Convey("Given feed syntax with nested rotated operands", t, func() {
		src := []byte(`[ { B(tokens[0,16]) { B(tokens[0,16] 8 <<) } & } ] <= [(B popcnt)]`)

		program, err := ParseFeedProgram(context.Background(), src)

		So(err, ShouldBeNil)
		So(program.HasFeed, ShouldBeTrue)
		So(len(program.Sites), ShouldEqual, 2)
		if err != nil || len(program.Sites) != 2 {
			return
		}

		operation := program.Sites[0].Operations[0]

		So(len(operation.Terms), ShouldEqual, 3)
		So(operation.Terms[0].Kind, ShouldEqual, TermCall)
		So(operation.Terms[0].Owner, ShouldEqual, "B")
		So(operation.Terms[0].Ref, ShouldEqual, "tokens[0,16]")
		So(operation.Terms[1].Kind, ShouldEqual, TermOperation)
		So(len(operation.Terms[1].Terms), ShouldEqual, 1)
		So(operation.Terms[1].Terms[0].Kind, ShouldEqual, TermCall)
		So(operation.Terms[1].Terms[0].Rotate, ShouldEqual, 8)
		So(operation.Terms[2].Kind, ShouldEqual, TermOperator)
		So(operation.Terms[2].Text, ShouldEqual, "&")

		So(program.Sites[1].CompactTerms[0].Kind, ShouldEqual, TermOwner)
		So(program.Sites[1].CompactTerms[1].Kind, ShouldEqual, TermReducer)
	})
}
