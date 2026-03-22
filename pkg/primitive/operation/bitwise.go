package operation

import (
	"io"
	"unsafe"

	"github.com/smallnest/ringbuffer"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Op is a binary bitwise function on uint64 slices.

Every implementation contains a hyper-optimized fast-path for the 128-word
(8192-bit) layout. By projecting the slice headers into fixed-size array pointers
and explicitly unrolling by 8, we guarantee that the compiler emits branchless,
maximally dense SIMD (AVX/NEON) vector instructions.
*/
type Op func(a, b, dst []uint64)

var (
	// OR is LCM (Least Common Multiple). Unions prime factors into a composite superset.
	OR Op = func(a, b, dst []uint64) {
		if len(dst) == primitive.Words && len(a) >= primitive.Words && len(b) >= primitive.Words {
			pa := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(a)))
			pb := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(b)))
			pd := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(dst)))

			for i := 0; i < primitive.Words; i += 8 {
				pd[i+0] = pa[i+0] | pb[i+0]
				pd[i+1] = pa[i+1] | pb[i+1]
				pd[i+2] = pa[i+2] | pb[i+2]
				pd[i+3] = pa[i+3] | pb[i+3]
				pd[i+4] = pa[i+4] | pb[i+4]
				pd[i+5] = pa[i+5] | pb[i+5]
				pd[i+6] = pa[i+6] | pb[i+6]
				pd[i+7] = pa[i+7] | pb[i+7]
			}
			return
		}

		n := len(dst)
		if n == 0 {
			return
		}
		_ = a[n-1]
		_ = b[n-1]
		for i := 0; i < n; i++ {
			dst[i] = a[i] | b[i]
		}
	}

	// AND is GCD (Greatest Common Divisor). Extracts shared structural prime factors.
	AND Op = func(a, b, dst []uint64) {
		if len(dst) == primitive.Words && len(a) >= primitive.Words && len(b) >= primitive.Words {
			pa := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(a)))
			pb := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(b)))
			pd := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(dst)))

			for i := 0; i < primitive.Words; i += 8 {
				pd[i+0] = pa[i+0] & pb[i+0]
				pd[i+1] = pa[i+1] & pb[i+1]
				pd[i+2] = pa[i+2] & pb[i+2]
				pd[i+3] = pa[i+3] & pb[i+3]
				pd[i+4] = pa[i+4] & pb[i+4]
				pd[i+5] = pa[i+5] & pb[i+5]
				pd[i+6] = pa[i+6] & pb[i+6]
				pd[i+7] = pa[i+7] & pb[i+7]
			}
			return
		}

		n := len(dst)
		if n == 0 {
			return
		}
		_ = a[n-1]
		_ = b[n-1]
		for i := 0; i < n; i++ {
			dst[i] = a[i] & b[i]
		}
	}

	// XOR is the symmetric factorization difference (LCM / GCD).
	// Measures multiplicative distance on the divisibility lattice.
	XOR Op = func(a, b, dst []uint64) {
		if len(dst) == primitive.Words && len(a) >= primitive.Words && len(b) >= primitive.Words {
			pa := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(a)))
			pb := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(b)))
			pd := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(dst)))

			for i := 0; i < primitive.Words; i += 8 {
				pd[i+0] = pa[i+0] ^ pb[i+0]
				pd[i+1] = pa[i+1] ^ pb[i+1]
				pd[i+2] = pa[i+2] ^ pb[i+2]
				pd[i+3] = pa[i+3] ^ pb[i+3]
				pd[i+4] = pa[i+4] ^ pb[i+4]
				pd[i+5] = pa[i+5] ^ pb[i+5]
				pd[i+6] = pa[i+6] ^ pb[i+6]
				pd[i+7] = pa[i+7] ^ pb[i+7]
			}
			return
		}

		n := len(dst)
		if n == 0 {
			return
		}
		_ = a[n-1]
		_ = b[n-1]
		for i := 0; i < n; i++ {
			dst[i] = a[i] ^ b[i]
		}
	}

	// AndNot (Material Nonimplication) isolates the unique factor residue.
	// Isolates the genuinely novel structure in 'a' that 'b' cannot account for.
	AndNot Op = func(a, b, dst []uint64) {
		if len(dst) == primitive.Words && len(a) >= primitive.Words && len(b) >= primitive.Words {
			pa := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(a)))
			pb := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(b)))
			pd := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(dst)))

			for i := 0; i < primitive.Words; i += 8 {
				pd[i+0] = pa[i+0] &^ pb[i+0]
				pd[i+1] = pa[i+1] &^ pb[i+1]
				pd[i+2] = pa[i+2] &^ pb[i+2]
				pd[i+3] = pa[i+3] &^ pb[i+3]
				pd[i+4] = pa[i+4] &^ pb[i+4]
				pd[i+5] = pa[i+5] &^ pb[i+5]
				pd[i+6] = pa[i+6] &^ pb[i+6]
				pd[i+7] = pa[i+7] &^ pb[i+7]
			}
			return
		}

		n := len(dst)
		if n == 0 {
			return
		}
		_ = a[n-1]
		_ = b[n-1]
		for i := 0; i < n; i++ {
			dst[i] = a[i] &^ b[i]
		}
	}

	NOT Op = func(a, _, dst []uint64) {
		if len(dst) == primitive.Words && len(a) >= primitive.Words {
			pa := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(a)))
			pd := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(dst)))

			for i := 0; i < primitive.Words; i += 8 {
				pd[i+0] = ^pa[i+0]
				pd[i+1] = ^pa[i+1]
				pd[i+2] = ^pa[i+2]
				pd[i+3] = ^pa[i+3]
				pd[i+4] = ^pa[i+4]
				pd[i+5] = ^pa[i+5]
				pd[i+6] = ^pa[i+6]
				pd[i+7] = ^pa[i+7]
			}
			return
		}

		n := len(dst)
		if n == 0 {
			return
		}
		_ = a[n-1]
		for i := 0; i < n; i++ {
			dst[i] = ^a[i]
		}
	}

	NAND Op = func(a, b, dst []uint64) {
		if len(dst) == primitive.Words && len(a) >= primitive.Words && len(b) >= primitive.Words {
			pa := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(a)))
			pb := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(b)))
			pd := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(dst)))

			for i := 0; i < primitive.Words; i += 8 {
				pd[i+0] = ^(pa[i+0] & pb[i+0])
				pd[i+1] = ^(pa[i+1] & pb[i+1])
				pd[i+2] = ^(pa[i+2] & pb[i+2])
				pd[i+3] = ^(pa[i+3] & pb[i+3])
				pd[i+4] = ^(pa[i+4] & pb[i+4])
				pd[i+5] = ^(pa[i+5] & pb[i+5])
				pd[i+6] = ^(pa[i+6] & pb[i+6])
				pd[i+7] = ^(pa[i+7] & pb[i+7])
			}
			return
		}

		n := len(dst)
		if n == 0 {
			return
		}
		_ = a[n-1]
		_ = b[n-1]
		for i := 0; i < n; i++ {
			dst[i] = ^(a[i] & b[i])
		}
	}

	NOR Op = func(a, b, dst []uint64) {
		if len(dst) == primitive.Words && len(a) >= primitive.Words && len(b) >= primitive.Words {
			pa := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(a)))
			pb := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(b)))
			pd := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(dst)))

			for i := 0; i < primitive.Words; i += 8 {
				pd[i+0] = ^(pa[i+0] | pb[i+0])
				pd[i+1] = ^(pa[i+1] | pb[i+1])
				pd[i+2] = ^(pa[i+2] | pb[i+2])
				pd[i+3] = ^(pa[i+3] | pb[i+3])
				pd[i+4] = ^(pa[i+4] | pb[i+4])
				pd[i+5] = ^(pa[i+5] | pb[i+5])
				pd[i+6] = ^(pa[i+6] | pb[i+6])
				pd[i+7] = ^(pa[i+7] | pb[i+7])
			}
			return
		}

		n := len(dst)
		if n == 0 {
			return
		}
		_ = a[n-1]
		_ = b[n-1]
		for i := 0; i < n; i++ {
			dst[i] = ^(a[i] | b[i])
		}
	}

	XNOR Op = func(a, b, dst []uint64) {
		if len(dst) == primitive.Words && len(a) >= primitive.Words && len(b) >= primitive.Words {
			pa := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(a)))
			pb := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(b)))
			pd := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(dst)))

			for i := 0; i < primitive.Words; i += 8 {
				pd[i+0] = ^(pa[i+0] ^ pb[i+0])
				pd[i+1] = ^(pa[i+1] ^ pb[i+1])
				pd[i+2] = ^(pa[i+2] ^ pb[i+2])
				pd[i+3] = ^(pa[i+3] ^ pb[i+3])
				pd[i+4] = ^(pa[i+4] ^ pb[i+4])
				pd[i+5] = ^(pa[i+5] ^ pb[i+5])
				pd[i+6] = ^(pa[i+6] ^ pb[i+6])
				pd[i+7] = ^(pa[i+7] ^ pb[i+7])
			}
			return
		}

		n := len(dst)
		if n == 0 {
			return
		}
		_ = a[n-1]
		_ = b[n-1]
		for i := 0; i < n; i++ {
			dst[i] = ^(a[i] ^ b[i])
		}
	}

	ConverseNonimplication Op = func(a, b, dst []uint64) {
		if len(dst) == primitive.Words && len(a) >= primitive.Words && len(b) >= primitive.Words {
			pa := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(a)))
			pb := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(b)))
			pd := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(dst)))

			for i := 0; i < primitive.Words; i += 8 {
				pd[i+0] = pb[i+0] &^ pa[i+0]
				pd[i+1] = pb[i+1] &^ pa[i+1]
				pd[i+2] = pb[i+2] &^ pa[i+2]
				pd[i+3] = pb[i+3] &^ pa[i+3]
				pd[i+4] = pb[i+4] &^ pa[i+4]
				pd[i+5] = pb[i+5] &^ pa[i+5]
				pd[i+6] = pb[i+6] &^ pa[i+6]
				pd[i+7] = pb[i+7] &^ pa[i+7]
			}
			return
		}

		n := len(dst)
		if n == 0 {
			return
		}
		_ = a[n-1]
		_ = b[n-1]
		for i := 0; i < n; i++ {
			dst[i] = b[i] &^ a[i]
		}
	}

	NotSecond Op = func(_, b, dst []uint64) {
		if len(dst) == primitive.Words && len(b) >= primitive.Words {
			pb := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(b)))
			pd := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(dst)))

			for i := 0; i < primitive.Words; i += 8 {
				pd[i+0] = ^pb[i+0]
				pd[i+1] = ^pb[i+1]
				pd[i+2] = ^pb[i+2]
				pd[i+3] = ^pb[i+3]
				pd[i+4] = ^pb[i+4]
				pd[i+5] = ^pb[i+5]
				pd[i+6] = ^pb[i+6]
				pd[i+7] = ^pb[i+7]
			}
			return
		}

		n := len(dst)
		if n == 0 {
			return
		}
		_ = b[n-1]
		for i := 0; i < n; i++ {
			dst[i] = ^b[i]
		}
	}

	MaterialConditional Op = func(a, b, dst []uint64) {
		if len(dst) == primitive.Words && len(a) >= primitive.Words && len(b) >= primitive.Words {
			pa := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(a)))
			pb := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(b)))
			pd := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(dst)))

			for i := 0; i < primitive.Words; i += 8 {
				pd[i+0] = ^pa[i+0] | pb[i+0]
				pd[i+1] = ^pa[i+1] | pb[i+1]
				pd[i+2] = ^pa[i+2] | pb[i+2]
				pd[i+3] = ^pa[i+3] | pb[i+3]
				pd[i+4] = ^pa[i+4] | pb[i+4]
				pd[i+5] = ^pa[i+5] | pb[i+5]
				pd[i+6] = ^pa[i+6] | pb[i+6]
				pd[i+7] = ^pa[i+7] | pb[i+7]
			}
			return
		}

		n := len(dst)
		if n == 0 {
			return
		}
		_ = a[n-1]
		_ = b[n-1]
		for i := 0; i < n; i++ {
			dst[i] = ^a[i] | b[i]
		}
	}

	ConverseImplication Op = func(a, b, dst []uint64) {
		if len(dst) == primitive.Words && len(a) >= primitive.Words && len(b) >= primitive.Words {
			pa := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(a)))
			pb := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(b)))
			pd := (*[primitive.Words]uint64)(unsafe.Pointer(unsafe.SliceData(dst)))

			for i := 0; i < primitive.Words; i += 8 {
				pd[i+0] = pa[i+0] | ^pb[i+0]
				pd[i+1] = pa[i+1] | ^pb[i+1]
				pd[i+2] = pa[i+2] | ^pb[i+2]
				pd[i+3] = pa[i+3] | ^pb[i+3]
				pd[i+4] = pa[i+4] | ^pb[i+4]
				pd[i+5] = pa[i+5] | ^pb[i+5]
				pd[i+6] = pa[i+6] | ^pb[i+6]
				pd[i+7] = pa[i+7] | ^pb[i+7]
			}
			return
		}

		n := len(dst)
		if n == 0 {
			return
		}
		_ = a[n-1]
		_ = b[n-1]
		for i := 0; i < n; i++ {
			dst[i] = a[i] | ^b[i]
		}
	}
)

/*
selectOp maps an instruction Value to its corresponding Op function.
The instruction Value's active bit positions encode the truth table index.
*/
func selectOp(instr *primitive.Value) Op {
	if instr.Has(16) {
		return MotorApply
	}
	if instr.Has(17) {
		return MotorInvert
	}
	if instr.Has(18) {
		return MotorCompose
	}

	// Check which truth table index is set
	if instr.Has(3) {
		return NOT
	}
	if instr.Has(2) {
		return ConverseNonimplication
	}
	if instr.Has(4) {
		return AndNot
	}
	if instr.Has(5) {
		return NotSecond
	}
	if instr.Has(6) {
		return XOR
	}
	if instr.Has(1) {
		return NOR
	}
	if instr.Has(7) {
		return NAND
	}
	if instr.Has(8) {
		return AND
	}
	if instr.Has(9) {
		return XNOR
	}
	if instr.Has(10) {
		return func(_, b, dst []uint64) {
			copy(dst, b)
		}
	}
	if instr.Has(11) {
		return MaterialConditional
	}
	if instr.Has(14) {
		return OR
	}
	if instr.Has(12) {
		return func(a, _, dst []uint64) {
			copy(dst, a)
		}
	}
	if instr.Has(13) {
		return ConverseImplication
	}
	if instr.Has(0) {
		return func(_, _ []uint64, dst []uint64) {
			clear(dst)
		}
	}
	if instr.Has(15) {
		return func(_, _ []uint64, dst []uint64) {
			for i := range dst {
				dst[i] = ^uint64(0)
			}
		}
	}

	// Default to AND if no recognized instruction
	return AND
}

/*
RollLeft circular-shifts all core bits left by shift positions.

MATHEMATICAL MAGIC:
Because CoreBits is 8191 (a Mersenne prime, precisely 128*64 - 1), a complex odd-bounded
circular shift reduces to an exact overlap of two infinite flat sequences. By treating
the array as an 8192-bit field, a shift of S over 8191 bits is strictly identical to:

	(src << S) | (src >> (8191 - S))

The missing 1 bit perfectly aligns the wrapped integer limits. This enables a fully
branchless O(1) pass over the memory space.
*/
func RollLeft(src, dst *primitive.Value, shift int) {
	shift = ((shift % primitive.CoreBits) + primitive.CoreBits) % primitive.CoreBits
	if shift == 0 {
		if src != dst {
			*dst = *src
		}
		return
	}

	s := shift
	r := primitive.CoreBits - s

	wordShiftL := s / 64
	bitShiftL := s % 64

	wordShiftR := r / 64
	bitShiftR := r % 64

	ps := (*[primitive.Words]uint64)(src)
	pd := (*[primitive.Words]uint64)(dst)

	// Stack allocated (zero cost overhead, handles dst == src aliasing transparently)
	var tmp [primitive.Words]uint64

	// 1. Left shift by S (treating as an 8192-bit integer)
	if bitShiftL == 0 {
		for i := wordShiftL; i < primitive.Words; i++ {
			tmp[i] = ps[i-wordShiftL]
		}
	} else {
		tmp[wordShiftL] = ps[0] << bitShiftL
		for i := wordShiftL + 1; i < primitive.Words; i++ {
			tmp[i] = (ps[i-wordShiftL] << bitShiftL) | (ps[i-wordShiftL-1] >> (64 - bitShiftL))
		}
	}

	// 2. Right shift by R (treating as an 8192-bit integer)
	if bitShiftR == 0 {
		for i := 0; i < primitive.Words-wordShiftR; i++ {
			tmp[i] |= ps[i+wordShiftR]
		}
	} else {
		for i := 0; i < primitive.Words-wordShiftR-1; i++ {
			tmp[i] |= (ps[i+wordShiftR] >> bitShiftR) | (ps[i+wordShiftR+1] << (64 - bitShiftR))
		}
		tmp[primitive.Words-wordShiftR-1] |= ps[primitive.Words-1] >> bitShiftR
	}

	// Clamp the trailing invalid overflow bit
	tmp[primitive.Words-1] &= primitive.LastMask

	*pd = tmp
}

/*
Bitwise is an io.ReadWriteCloser that applies one binary bitwise
operation as an accumulator. Each Write clicks the accumulator
forward. Read returns the current state at any time.
*/
type Bitwise struct {
	op   Op
	ring *ringbuffer.RingBuffer
	ret  error
}

/*
NewBitwise creates a Bitwise accumulator with the given Op.
*/
func NewBitwise(op Op) *Bitwise {
	return &Bitwise{
		op:   op,
		ring: ringbuffer.New(4 * primitive.ByteSize),
	}
}

/*
Op returns the current operation function.
*/
func (bitwise *Bitwise) Op() Op {
	return bitwise.op
}

/*
Read performs the operation on the two stored operands and returns the result.
Only valid when exactly 3 frames (Instruction + 2 Operands) are in the ringbuffer.
*/
func (bitwise *Bitwise) Read(p []byte) (n int, err error) {
	length := bitwise.ring.Length()

	if length == 0 {
		return 0, io.EOF
	}

	// Block until Instruction + 2 operand frames (3 × 1024 bytes) are present.
	if length < 3*primitive.ByteSize {
		return 0, nil
	}

	if len(p) < primitive.ByteSize {
		return 0, primitive.ErrShortValue
	}

	buf := make([]byte, 3*primitive.ByteSize)
	_, _ = bitwise.ring.Read(buf)

	instr := primitive.NewValueFromBytes(buf[0:primitive.ByteSize])
	a := primitive.NewValueFromBytes(buf[primitive.ByteSize : 2*primitive.ByteSize])
	b := primitive.NewValueFromBytes(buf[2*primitive.ByteSize : 3*primitive.ByteSize])

	op := selectOp(instr)

	result := primitive.NewValue()
	op(a[:], b[:], result[:])
	result.Clamp()

	n, err = result.Read(p)

	if err == io.EOF {
		bitwise.ring.Reset()

		return n, nil
	}

	return n, err
}

/*
Write drops packets strictly onto the RingBuffer. The operation logic
is exclusively deferred until the 3-frame sequence resolves.
*/
func (bitwise *Bitwise) Write(p []byte) (n int, err error) {
	if len(p) < primitive.ByteSize {
		return 0, primitive.ErrShortValue
	}

	// 0-alloc mapping of[]byte straight onto the incoming register.
	// We drop safe multi-arch Endian mapping for sheer physical memory speed.
	incoming := (*primitive.Value)(unsafe.Pointer(unsafe.SliceData(p)))

	if bitwise.ring.Length() == 0 {
		bitwise.op = selectOp(incoming)
	}

	_, err = bitwise.ring.Write(unsafe.Slice((*byte)(unsafe.Pointer(&incoming[0])), primitive.ByteSize))

	// Explicitly return primitive.ByteSize to ensure standard io interfaces advance
	return primitive.ByteSize, err
}

/*
Close resets the accumulator to zero state.
*/
func (bitwise *Bitwise) Close() error {
	bitwise.ring.Reset()
	return nil
}
