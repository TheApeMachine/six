package primitive

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
)

func TestTokenAttentionMaskForWord(t *testing.T) {

	Convey("TokenAttentionMaskForWord", t, func() {
		savedR0 := core.Cfg.Value.Region.Registers.R0
		savedR1 := core.Cfg.Value.Region.Registers.R1
		savedR2 := core.Cfg.Value.Region.Registers.R2
		savedR3 := core.Cfg.Value.Region.Registers.R3
		savedWords := core.Cfg.Value.Words
		defer func() {
			core.Cfg.Value.Region.Registers.R0 = savedR0
			core.Cfg.Value.Region.Registers.R1 = savedR1
			core.Cfg.Value.Region.Registers.R2 = savedR2
			core.Cfg.Value.Region.Registers.R3 = savedR3
			core.Cfg.Value.Words = savedWords
		}()

		core.Cfg.Value.Words = 128
		core.Cfg.Value.Region.Registers.R0 = 10
		core.Cfg.Value.Region.Registers.R1 = 11
		core.Cfg.Value.Region.Registers.R2 = 12
		core.Cfg.Value.Region.Registers.R3 = 13

		var frame Value
		So(TokenAttentionMaskForWord(&frame, 0), ShouldEqual, ^uint64(0))

		frame[10] = 0xf
		frame[11] = 0xf0
		frame[12] = 0xA00
		frame[13] = 0xB00

		So(TokenAttentionMaskForWord(&frame, 0), ShouldEqual, frame[10])
		So(TokenAttentionMaskForWord(&frame, 1), ShouldEqual, frame[11])
		So(TokenAttentionMaskForWord(&frame, 2), ShouldEqual, frame[12])
		So(TokenAttentionMaskForWord(&frame, 3), ShouldEqual, frame[13])
	})
}

var benchAttentionMaskSink uint64

func BenchmarkTokenAttentionMaskForWord(b *testing.B) {
	savedR0 := core.Cfg.Value.Region.Registers.R0
	savedR1 := core.Cfg.Value.Region.Registers.R1
	savedR2 := core.Cfg.Value.Region.Registers.R2
	savedR3 := core.Cfg.Value.Region.Registers.R3
	savedWords := core.Cfg.Value.Words
	defer func() {
		core.Cfg.Value.Region.Registers.R0 = savedR0
		core.Cfg.Value.Region.Registers.R1 = savedR1
		core.Cfg.Value.Region.Registers.R2 = savedR2
		core.Cfg.Value.Region.Registers.R3 = savedR3
		core.Cfg.Value.Words = savedWords
	}()

	core.Cfg.Value.Words = 128
	core.Cfg.Value.Region.Registers.R0 = 10
	core.Cfg.Value.Region.Registers.R1 = 11
	core.Cfg.Value.Region.Registers.R2 = 12
	core.Cfg.Value.Region.Registers.R3 = 13

	var frame Value
	frame[10] = 0xff
	frame[11] = 0xff
	frame[12] = 0xff
	frame[13] = 0xff

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		benchAttentionMaskSink = TokenAttentionMaskForWord(&frame, iteration%4)
	}
}
