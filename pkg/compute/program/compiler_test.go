package program

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
)

func layoutLikeConfigViper() Layout {
	return Layout{
		Regions: map[string]RegionExtent{
			"tokens":     {Start: 0, Words: 16},
			"program":    {Start: 16, Words: 16},
			"signals":    {Start: 32, Words: 8},
			"context":    {Start: 40, Words: 8},
			"gradient":   {Start: 48, Words: 8},
			"properties": {Start: 56, Words: 16},
			"asset":      {Start: 72, Words: 48},
			"prev":       {Start: 120, Words: 1},
			"next":       {Start: 121, Words: 1},
			"id":         {Start: 122, Words: 1},
			"affinity":   {Start: 123, Words: 5},
		},
		Properties: map[string]int{
			"labels": 0, "confidence": 1, "epoch": 2, "ttl": 3, "temperature": 4, "status": 5, "noise": 6, "program_id": 7,
			"community": 8, "target": 9, "role": 10, "reference": 11, "surprisal": 12, "prev_surprisal": 13, "delta_surprisal": 14,
			"continuation": 15,
		},
	}
}

func TestStructuralComponentProgramMatchesContract(t *testing.T) {
	t.Parallel()
	lay := layoutLikeConfigViper()
	src := `
<[
  { B(prev) B(id) ^ }
  { B(next) B(id) ^ }
] <= [
  { B(tokens) B(signals[0,1]) ^ }
  { B(tokens) B(signals[1,1]) ^ }
  { B(tokens) B(signals[2,1]) ^ }
]> [
  { B(signals) }
] <= [
  { B(tokens[0,16]) B(tokens[0,16]) & }
  { B(tokens[0,16]) { B(tokens[0,16] 8 <<) } & }
]
`
	out, err := Compile(src, lay)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(out.Words) != 8 {
		t.Fatalf("expected 8 words, got %d", len(out.Words))
	}

	_, _, _, _, dst, _, opcode, mode, _, _, _, _, bType := DecodeInstruction(out.Words[0])
	if dst != 0 || opcode != Opcodes["&"] || mode != ModeTruth || bType != InstrBTypeDirect || out.Words[0]&InstrFlagAFromB == 0 {
		t.Fatalf("rightmost pipe word 0 = dst %d opcode 0x%x mode %d bType %d flags 0x%x", dst, opcode, mode, bType, out.Words[0]>>60)
	}

	_, _, _, _, dst, _, _, mode, topology, _, _, _, _ := DecodeInstruction(out.Words[2])
	if dst != 32 || mode != ModeTruth || topology != TopologySelf || out.Words[2]&InstrFlagAFromB == 0 {
		t.Fatalf("materialize pipe word = dst %d mode %d topology %d flags 0x%x", dst, mode, topology, out.Words[2]>>60)
	}

	_, _, bStart, _, dst, _, opcode, mode, _, _, _, _, bType := DecodeInstruction(out.Words[3])
	if dst != 0 || bStart != 32 || opcode != Opcodes["^"] || mode != ModeEmit || bType != InstrBTypeDirect || out.Words[3]&InstrFlagTargetB == 0 {
		t.Fatalf("middle pipe word = dst %d bStart %d opcode 0x%x mode %d bType %d flags 0x%x", dst, bStart, opcode, mode, bType, out.Words[3]>>60)
	}

	_, _, _, _, dst, _, opcode, mode, _, _, _, _, bType = DecodeInstruction(out.Words[6])
	if dst != 120 || opcode != Opcodes["^"] || mode != ModeEmit || bType != InstrBTypeDirect || out.Words[6]&InstrFlagTargetB == 0 {
		t.Fatalf("leftmost pipe word = dst %d opcode 0x%x mode %d bType %d flags 0x%x", dst, opcode, mode, bType, out.Words[6]>>60)
	}
}

func TestConfigProgramsCompile(t *testing.T) {
	t.Parallel()

	cfg := viper.New()
	cfg.SetConfigFile(filepath.Join("..", "..", "..", "cmd", "cfg", "config.yml"))
	if err := cfg.ReadInConfig(); err != nil {
		t.Fatalf("read config: %v", err)
	}

	programs := cfg.GetStringMapString("programs")
	if len(programs) == 0 {
		t.Fatalf("expected config programs")
	}

	lay := layoutLikeConfigViper()
	replacer := configProgramReplacer(cfg)

	for name, source := range programs {
		source := source
		if source == "" {
			continue
		}

		t.Run(name, func(t *testing.T) {
			out, err := Compile(replacer.Replace(source), lay)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if len(out.Words) == 0 {
				t.Fatalf("expected instructions")
			}
			if len(out.Words) > lay.Regions["program"].Words {
				t.Fatalf("program lowered to %d words; program region holds %d", len(out.Words), lay.Regions["program"].Words)
			}
		})
	}
}

func configProgramReplacer(cfg *viper.Viper) *strings.Replacer {
	shannonThreshold := int(cfg.GetFloat64("system.shannonLimit") * 256)
	if shannonThreshold < 0 {
		shannonThreshold = 0
	}
	if shannonThreshold > 256 {
		shannonThreshold = 256
	}

	routeBudget := cfg.GetInt("system.routeBudget")
	if routeBudget == 0 {
		routeBudget = 128
	}

	return strings.NewReplacer(
		"{{shannonLimitPopcount}}", strconv.Itoa(shannonThreshold),
		"{{routeBudget}}", strconv.Itoa(routeBudget),
	)
}

func TestFeedExampleProgramMatchesContract(t *testing.T) {
	t.Parallel()
	lay := layoutLikeConfigViper()
	out, err := Compile(`[(B popcnt)] <= [(A B ^)]`, lay)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(out.Words) != 2 {
		t.Fatalf("expected 2 words, got %d", len(out.Words))
	}
}

func TestFeedBareABImplicitMapTargetsB(t *testing.T) {
	t.Parallel()

	lay := layoutLikeConfigViper()
	out, err := Compile(`[(B popcnt)] <= [(A B ^)]`, lay)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(out.Words) != 2 {
		t.Fatalf("expected 2 words, got %d", len(out.Words))
	}

	_, _, _, _, dstStart, dstSpan, opcode, mode, _, _, _, _, bType := DecodeInstruction(out.Words[0])
	if dstStart != 32 || dstSpan != 8 || opcode != Opcodes["^"] || mode != ModeTruth || bType != InstrBTypeDirect {
		t.Fatalf("unexpected implicit map op: dst=%d/%d opcode=0x%x mode=%d bType=%d", dstStart, dstSpan, opcode, mode, bType)
	}
	if out.Words[0]&InstrFlagTargetB == 0 || out.Words[0]&InstrFlagTargetOwner != 0 || out.Words[0]&InstrFlagAFromB != 0 {
		t.Fatalf("expected bare A/B map to target B from owner A, got flags 0x%x", out.Words[0]>>60)
	}

	_, _, _, _, dstStart, dstSpan, opcode, mode, _, _, _, _, bType = DecodeInstruction(out.Words[1])
	if dstStart != 32 || dstSpan != 8 || opcode != Opcodes["A"] || mode != ModePopcnt || bType != InstrBTypeDirect {
		t.Fatalf("unexpected mapped reducer: dst=%d/%d opcode=0x%x mode=%d bType=%d", dstStart, dstSpan, opcode, mode, bType)
	}
	if out.Words[1]&InstrFlagTargetB == 0 || out.Words[1]&InstrFlagAFromB == 0 {
		t.Fatalf("expected B reducer to map over B frames, got flags 0x%x", out.Words[1]>>60)
	}
}

func TestFeedReducerStoreMatchesContract(t *testing.T) {
	t.Parallel()

	lay := layoutLikeConfigViper()
	out, err := Compile(`[ { A(surprisal) A(signals[0,8]) popcnt } ]`, lay)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(out.Words) != 1 {
		t.Fatalf("expected 1 word, got %d", len(out.Words))
	}

	aStart, aSpan, bStart, bSpan, dstStart, dstSpan, opcode, mode, _, _, _, _, bType := DecodeInstruction(out.Words[0])
	if aStart != 32 || aSpan != 8 || bStart != 0 || bSpan != 1 || dstStart != 68 || dstSpan != 1 {
		t.Fatalf("unexpected spans: a=%d/%d b=%d/%d dst=%d/%d", aStart, aSpan, bStart, bSpan, dstStart, dstSpan)
	}
	if opcode != Opcodes["A"] || mode != ModePopcnt || bType != InstrBTypeDirect {
		t.Fatalf("unexpected reducer lowering: opcode=0x%x mode=%d bType=%d", opcode, mode, bType)
	}
}

func TestFeedExplicitPropertyMatchesContract(t *testing.T) {
	t.Parallel()

	lay := layoutLikeConfigViper()
	out, err := Compile(`[ { B(signals[0,1]) B(program_id) B } ]`, lay)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(out.Words) != 1 {
		t.Fatalf("expected 1 word, got %d", len(out.Words))
	}

	_, _, bStart, bSpan, dstStart, dstSpan, opcode, mode, _, _, _, _, bType := DecodeInstruction(out.Words[0])
	if bStart != 63 || bSpan != 1 || dstStart != 32 || dstSpan != 1 {
		t.Fatalf("unexpected property lowering: b=%d/%d dst=%d/%d", bStart, bSpan, dstStart, dstSpan)
	}
	if opcode != Opcodes["B"] || mode != ModeTruth || bType != InstrBTypeDirect {
		t.Fatalf("unexpected direct property read: opcode=0x%x mode=%d bType=%d", opcode, mode, bType)
	}
	if out.Words[0]&InstrFlagTargetB == 0 || out.Words[0]&InstrFlagAFromB == 0 {
		t.Fatalf("expected B target/source flags, got 0x%x", out.Words[0]>>60)
	}
}

func TestFeedIndexedOperandAllowsWhitespace(t *testing.T) {
	t.Parallel()

	lay := layoutLikeConfigViper()
	out, err := Compile(`[ { B(tokens) B(signals[0, 1]) ^ } ]`, lay)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(out.Words) != 1 {
		t.Fatalf("expected 1 word, got %d", len(out.Words))
	}

	_, _, bStart, bSpan, dstStart, dstSpan, opcode, mode, _, _, _, _, bType := DecodeInstruction(out.Words[0])
	if bStart != 32 || bSpan != 1 || dstStart != 0 || dstSpan != 16 {
		t.Fatalf("unexpected spans: b=%d/%d dst=%d/%d", bStart, bSpan, dstStart, dstSpan)
	}
	if opcode != Opcodes["^"] || mode != ModeTruth || bType != InstrBTypeDirect {
		t.Fatalf("unexpected lowering: opcode=0x%x mode=%d bType=%d", opcode, mode, bType)
	}
}

func TestFeedBarePropertyRequiresOwner(t *testing.T) {
	t.Parallel()

	lay := layoutLikeConfigViper()
	if _, err := Compile(`[ { B(signals[0,1]) program_id B } ]`, lay); err == nil {
		t.Fatalf("expected bare property to require A(...) or B(...)")
	}
}

func TestFeedFoldTopologyMatchesContract(t *testing.T) {
	t.Parallel()

	lay := layoutLikeConfigViper()
	out, err := Compile(`[ { A(signals[0,5]) B(affinity[0,5]) | fold } ]`, lay)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(out.Words) != 1 {
		t.Fatalf("expected 1 word, got %d", len(out.Words))
	}

	_, _, bStart, bSpan, dstStart, dstSpan, opcode, mode, topology, _, _, _, bType := DecodeInstruction(out.Words[0])
	if bStart != 123 || bSpan != 5 || dstStart != 32 || dstSpan != 5 {
		t.Fatalf("unexpected spans: b=%d/%d dst=%d/%d", bStart, bSpan, dstStart, dstSpan)
	}
	if opcode != Opcodes["|"] || mode != ModeTruth || topology != TopologyFold || bType != InstrBTypeDirect {
		t.Fatalf("unexpected fold lowering: opcode=0x%x mode=%d topology=%d bType=%d", opcode, mode, topology, bType)
	}
	if out.Words[0]&InstrFlagTargetOwner == 0 {
		t.Fatalf("expected fold target to stay on owner, got flags 0x%x", out.Words[0]>>60)
	}
}

func TestFeedAssetIndexedOperandIsRegionRelative(t *testing.T) {
	Convey("Given an indexed asset operand in feed syntax", t, func() {
		lay := layoutLikeConfigViper()

		out, err := Compile(`[ { B(asset[0,5]) B(affinity[0,5]) B } ]`, lay)

		So(err, ShouldBeNil)
		So(len(out.Words), ShouldEqual, 1)
		if err != nil || len(out.Words) != 1 {
			return
		}

		_, _, bStart, bSpan, dstStart, dstSpan, opcode, mode, _, _, _, _, bType := DecodeInstruction(out.Words[0])

		So(dstStart, ShouldEqual, 72)
		So(dstSpan, ShouldEqual, 5)
		So(bStart, ShouldEqual, 123)
		So(bSpan, ShouldEqual, 5)
		So(opcode, ShouldEqual, Opcodes["B"])
		So(mode, ShouldEqual, ModeTruth)
		So(bType, ShouldEqual, InstrBTypeDirect)
	})
}

func TestFeedThresholdGateBlocksAtThreshold(t *testing.T) {
	Convey("Given a feed threshold gate", t, func() {
		lay := layoutLikeConfigViper()
		source := `
[ { B(asset[6,1]) A(id) B } ] <= [
  { { B(asset[0,5]) popcnt } 120 ? }
] <= [
  { B(asset[0,5]) B(affinity[0,5]) B }
]`

		out, err := Compile(source, lay)

		So(err, ShouldBeNil)
		So(len(out.Words), ShouldEqual, 2)
		if err != nil || len(out.Words) != 2 {
			return
		}

		_, _, _, _, dstStart, _, _, _, _, predStart, predCond, _, _ := DecodeInstruction(out.Words[1])

		So(dstStart, ShouldEqual, 78)
		So(predCond, ShouldEqual, predExtended)

		var frame [128]uint64
		frame[72] = ^uint64(0)
		frame[73] = (uint64(1) << 55) - 1

		So(PredicateAllows(&frame, predStart, predCond), ShouldBeTrue)

		frame[73] = (uint64(1) << 56) - 1

		So(PredicateAllows(&frame, predStart, predCond), ShouldBeFalse)
	})
}

func TestFeedPopcntPredicateKeepsAtMostSemantics(t *testing.T) {
	Convey("Given a feed pipe with popcnt threshold predicate", t, func() {
		lay := layoutLikeConfigViper()

		out, err := Compile(`[ { A(signals[0,1]) 1 B } { A(affinity[0,5]) popcnt 121 ? } ]`, lay)

		So(err, ShouldBeNil)
		So(len(out.Words), ShouldEqual, 1)
		if err != nil || len(out.Words) != 1 {
			return
		}

		_, _, _, _, _, _, _, _, _, predStart, predCond, _, _ := DecodeInstruction(out.Words[0])

		So(predCond, ShouldEqual, predExtended)

		var frame [128]uint64
		frame[123] = ^uint64(0)
		frame[124] = (uint64(1) << 56) - 1

		So(PredicateAllows(&frame, predStart, predCond), ShouldBeTrue)

		frame[124] = (uint64(1) << 57) - 1

		So(PredicateAllows(&frame, predStart, predCond), ShouldBeFalse)
	})
}

func TestCompiler(t *testing.T) {
	lay := Layout{
		Regions: map[string]RegionExtent{
			"program":    {Start: 16, Words: 16},
			"signals":    {Start: 32, Words: 8},
			"context":    {Start: 40, Words: 8},
			"gradient":   {Start: 48, Words: 8},
			"properties": {Start: 56, Words: 16},
			"rom":        {Start: 100, Words: 16},
			"affinity":   {Start: 123, Words: 5},
		},
		Properties: map[string]int{
			"continuation": 15,
			"unsupervised": 0,
		},
	}

	tests := []struct {
		name      string
		src       string
		valid     bool
		wantWords int
	}{
		{
			name:      "Basic Local Sweep",
			src:       `[ { A(16..24) B(0..8) B(8..16) ^ } ]`,
			valid:     true,
			wantWords: 1,
		},
		{
			name: "Predicated Fold",
			src: `
[ { { A(signals[7,1]) 0 != } ? } ]
[ { A(gradient) B(context) ^ fold } ]`,
			valid:     true,
			wantWords: 1,
		},
		{
			name:      "Fold rejects projection",
			src:       `[ { A(gradient) B(context) A fold } ]`,
			valid:     false,
			wantWords: 0,
		},
		{
			name:      "Fold rejects NAND",
			src:       `[ { A(gradient) B(context) B(signals) ~& fold } ]`,
			valid:     false,
			wantWords: 0,
		},
		{
			name:      "Fold accepts XOR",
			src:       `[ { A(gradient) B(context) ^ fold } ]`,
			valid:     true,
			wantWords: 1,
		},
		{
			name:      "Reduction",
			src:       `[ { A(signals[7,1]) B(16..24) B(context) -> any_zero } ]`,
			valid:     true,
			wantWords: 1,
		},
		{
			name: "Immediate with predicate",
			src: `
[ { { A(signals[7,1]) 0 != } ? } ]
[ { A(program) B(rom.unsupervised) 0 | } ]`,
			valid:     true,
			wantWords: 1,
		},
		{
			name:      "Popcnt threshold predicate",
			src:       `[ { A(signals[7,1]) 1 B } { A(affinity[0,5]) popcnt 121 ? } ]`,
			valid:     true,
			wantWords: 1,
		},
		{
			name:      "Bare A expression (must not eat closing bracket)",
			src:       `[ { A(signals[7,1]) A } ]`,
			valid:     true,
			wantWords: 1,
		},
		{
			name:      "Saturates is not a language intrinsic",
			src:       `[ { A(signals[7,1]) B(affinity[0,5]) saturates } ]`,
			valid:     false,
			wantWords: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiled, err := Compile(tt.src, lay)
			if tt.valid && err != nil {
				t.Fatalf("expected valid, got error: %v", err)
			}
			if !tt.valid && err == nil {
				t.Fatalf("expected error, got valid")
			}
			if tt.valid && tt.wantWords > 0 && len(compiled.Words) != tt.wantWords {
				t.Fatalf("expected %d words, got %d", tt.wantWords, len(compiled.Words))
			}
		})
	}
}

func BenchmarkFeedThresholdGateCompile(b *testing.B) {
	lay := layoutLikeConfigViper()
	source := `
[ { B(asset[6,1]) A(id) B } ] <= [
  { { B(asset[0,5]) popcnt } 120 ? }
] <= [
  { B(asset[0,5]) B(affinity[0,5]) B }
]`

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		if _, err := Compile(source, lay); err != nil {
			b.Fatal(err)
		}
	}
}
