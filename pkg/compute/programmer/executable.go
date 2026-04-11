package programmer

import (
	"encoding/binary"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unsafe"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Executable binds compilation to execution: a Compiler plus optional ingress
Values and an optional finalizer that can emit follow-on Values (routing,
control flow, derived payloads).

Inputs carry token/signal/affinity (or any full wire) into the run: each
emitted Value starts as a copy of inputs[0] when present, then the compiled
program words for that frame overwrite the program region. Additional inputs
are reserved for multi-operand staging later.
*/
type Executable struct {
	compiler  *Compiler
	inputs    []*primitive.Value
	finalizer func(*primitive.Value) ([]*primitive.Value, error)
}

func NewExecutable(
	compiler *Compiler,
	finalizer func(*primitive.Value) ([]*primitive.Value, error),
) *Executable {
	return &Executable{compiler: compiler, finalizer: finalizer}
}

/*
Inputs returns the ingress slice (may be nil). Callers own the Values.
*/
func (executable *Executable) Inputs() []*primitive.Value {
	return executable.inputs
}

/*
Execute compiles and materializes one unsafe.Pointer per compiled frame: each
pointer is the base of a full primitive.Value suitable for cpu/metal/cuda
Substrate.Execute. When inputs[0] is non-nil, its full wire frame is copied
into each output before operand bands and the program region are filled from
the token and Frame. Scheduling from the optional continuation is written on
the last emitted Value (word 117).
*/
func (executable *Executable) Execute(target CompilerTarget) ([]unsafe.Pointer, error) {
	frames, err := executable.compiler.Compile(target)

	if err != nil {
		return nil, err
	}

	tokens := executable.compiler.Tokens()

	if len(tokens) != len(frames) {
		return nil, fmt.Errorf("programmer: token/frame count mismatch: %d tokens, %d frames",
			len(tokens), len(frames))
	}

	out := make([]unsafe.Pointer, 0, len(frames))
	cont := executable.compiler.Continuation()
	bands := newOperandBands()

	for idx := range frames {
		value := executable.valueForFrame()

		bands.fill(value, tokens[idx])
		frames[idx].writeIntoProgramRegion(value)

		if idx == len(frames)-1 {
			cont.ApplyScheduling(value)
		}

		out = append(out, unsafe.Pointer(&(*value)[0]))
	}

	return out, nil
}

/*
Finalize runs the finalizer on one post-execution Value. When finalizer is nil,
returns a single-element slice containing that Value.
*/
func (executable *Executable) Finalize(out *primitive.Value) ([]*primitive.Value, error) {
	if executable.finalizer == nil {
		return []*primitive.Value{out}, nil
	}

	return executable.finalizer(out)
}

/*
WithInputs attaches Values whose wire frames seed emitted Values before the
compiled program is written. Returns the same Executable for chaining.
*/
func (executable *Executable) WithInputs(inputs []*primitive.Value) *Executable {
	executable.inputs = inputs

	return executable
}

/*
valueForFrame mints a Value wire for one frame: a copy of inputs[0] when set,
otherwise a zero wire.
*/
func (executable *Executable) valueForFrame() *primitive.Value {
	if len(executable.inputs) > 0 && executable.inputs[0] != nil {
		v := *executable.inputs[0]

		return &v
	}

	out := primitive.Value{}

	return &out
}

/*
operandBands maps region refs into the operand lanes universalBitwiseV2 reads.
*/
type operandBands struct {
	refRE *regexp.Regexp
}

func newOperandBands() *operandBands {
	return &operandBands{refRE: regexp.MustCompile(`^([a-z]+)\[([0-9,]+)\]$`)}
}

func (bands *operandBands) fill(value *primitive.Value, tok Token) {
	if value == nil {
		return
	}

	refA, errA := bands.parseRef(tok.SrcA)
	refB, errB := bands.parseRef(tok.SrcB)

	if errA == nil && refA.name == "tokens" && len(refA.indices) > 0 {
		bands.packQueryWords(value, refA.indices)
	}

	if errB == nil && refB.name == "tokens" && len(refB.indices) > 0 {
		bands.packBRotations(value, refB.indices)
	}
}

type regionRef struct {
	name    string
	indices []int
}

func (bands *operandBands) parseRef(label string) (regionRef, error) {
	m := bands.refRE.FindStringSubmatch(strings.TrimSpace(label))

	if len(m) != 3 {
		return regionRef{}, fmt.Errorf("programmer: invalid region ref %q", label)
	}

	parts := strings.Split(m[2], ",")
	indices := make([]int, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)

		if part == "" {
			continue
		}

		idx, err := strconv.Atoi(part)

		if err != nil {
			return regionRef{}, fmt.Errorf("programmer: invalid region ref %q", label)
		}

		indices = append(indices, idx)
	}

	if len(indices) == 0 {
		return regionRef{}, fmt.Errorf("programmer: invalid region ref %q", label)
	}

	return regionRef{name: m[1], indices: indices}, nil
}

func (*operandBands) tokenCodeAt(value *primitive.Value, slot int) uint16 {
	if value == nil || slot < 0 {
		return 0
	}

	startWord := core.Cfg.Value.Region.Tokens.Start
	off := startWord*8 + slot*2
	buf := value.Bytes()

	if off+2 > len(buf) {
		return 0
	}

	return binary.LittleEndian.Uint16(buf[off:])
}

func (bands *operandBands) packQueryWords(value *primitive.Value, indices []int) {
	var words [4]uint64

	for slotIdx, tokIdx := range indices {
		if slotIdx >= 16 {
			break
		}

		code := uint64(bands.tokenCodeAt(value, tokIdx))
		word := slotIdx / 4
		shift := uint((slotIdx % 4) * 16)
		words[word] |= code << shift
	}

	for wordIdx := 0; wordIdx < 4; wordIdx++ {
		value.Set(wordIdx, words[wordIdx])
	}
}

func (bands *operandBands) packBRotations(value *primitive.Value, indices []int) {
	if len(indices) == 0 {
		return
	}

	var b0, b1, b2, b3 uint64

	for i := 0; i < 4 && i < len(indices); i++ {
		code := uint64(bands.tokenCodeAt(value, indices[i%len(indices)]))

		switch i {
		case 0:
			b0 = code
		case 1:
			b1 = code
		case 2:
			b2 = code
		case 3:
			b3 = code
		}
	}

	for rotation := 0; rotation < 16; rotation++ {
		base := 32 + rotation*4
		value.Set(base+0, b0)
		value.Set(base+1, b1)
		value.Set(base+2, b2)
		value.Set(base+3, b3)
	}
}
