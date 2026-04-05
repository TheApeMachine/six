//go:generate go run gen.go
package primitive

import (
	"io"
	"sync"
	"unsafe"

	"github.com/theapemachine/six/pkg/core"
)

const valueScratchCap = 1024

var (
	valueTo   func(*Value, []byte)
	valueFrom func([]byte, *Value)
)

/*
Value is a programmable type that acts as a token for
machine intelligence.

Governed by the following rules:

- Value computes on itself
- Value encounters alter computation
*/
type Value [128]uint64

func init() {
	x := uint16(1)

	// Fast-path memory alignment for little-endian architectures
	if *(*byte)(unsafe.Pointer(&x)) == 1 {
		valueTo = func(v *Value, p []byte) {
			copy(p, unsafe.Slice(
				(*byte)(unsafe.Pointer(&v[0])),
				core.Cfg.Value.Bytes,
			))
		}
		valueFrom = func(p []byte, v *Value) {
			copy(unsafe.Slice(
				(*byte)(unsafe.Pointer(&v[0])),
				core.Cfg.Value.Bytes,
			), p)
		}
		return
	}

	valueTo = valueToPortable
	valueFrom = valueFromPortable
}

func valueToPortable(v *Value, p []byte) {
	for i := range core.Cfg.Value.Words {
		p[i*8] = byte(v[i])
		p[i*8+1] = byte(v[i] >> 8)
		p[i*8+2] = byte(v[i] >> 16)
		p[i*8+3] = byte(v[i] >> 24)
		p[i*8+4] = byte(v[i] >> 32)
		p[i*8+5] = byte(v[i] >> 40)
		p[i*8+6] = byte(v[i] >> 48)
		p[i*8+7] = byte(v[i] >> 56)
	}
}

func valueFromPortable(p []byte, v *Value) {
	for i := range core.Cfg.Value.Words {
		v[i] = uint64(p[i*8]) |
			uint64(p[i*8+1])<<8 |
			uint64(p[i*8+2])<<16 |
			uint64(p[i*8+3])<<24 |
			uint64(p[i*8+4])<<32 |
			uint64(p[i*8+5])<<40 |
			uint64(p[i*8+6])<<48 |
			uint64(p[i*8+7])<<56
	}
}

/*
valuePool is a global pool of Values. It is used to recycle
Values that have been closed. It should be safe to re-use ValueIDs
because the Tomstone program should be clearing up any references to
a discarded ValueID. Values should ALWAYS be tombstoned, never just
discarded. You will receive an error from Close if you try to close
a Value that has not been properly tombstoned.
*/
var valuePool = sync.Pool{
	New: func() any {
		return &Value{}
	},
}

/*
NewValue should only be used to create the initial Value.
This method should not be used to create temporary Values.
The returned pointer is owned by the caller until Close
returns it to valuePool.
*/
func NewValue(p []byte) (*Value, error) {
	raw := valuePool.Get()
	v := raw.(*Value)
	*v = Value{}

	byteLen := core.Cfg.Value.Bytes

	if byteLen <= 0 || len(p) == 0 {
		return v, nil
	}

	n := min(len(p), byteLen)

	if byteLen <= valueScratchCap {
		var scratch [valueScratchCap]byte
		buf := scratch[:byteLen]
		copy(buf, p[:n])
		valueFrom(buf, v)

		return v, nil
	}

	buf := make([]byte, byteLen)
	copy(buf, p[:n])
	valueFrom(buf, v)

	return v, nil
}

/*
Read implements io.Reader, which prepares the Value for
transmission over the wire.
It is important to understand that we do not pay any
traditional serialization tax, because the Value is already
serialized in memory.
*/
func (value *Value) Read(p []byte) (int, error) {
	if len(p) < core.Cfg.Value.Bytes {
		return 0, io.ErrShortBuffer
	}

	valueTo(value, p)
	return core.Cfg.Value.Bytes, io.EOF
}

/*
Write implements io.Writer, which convert the Value from
its wire format into a Value type.
It is important to understand that we do not pay any
traditional serialization tax, because the Value is already
serialized in memory. This is the same as Read, but in reverse.
*/
func (value *Value) Write(p []byte) (int, error) {
	if len(p) < core.Cfg.Value.Bytes {
		return 0, io.ErrShortBuffer
	}

	valueFrom(p, value)
	return core.Cfg.Value.Bytes, nil
}

/*
Close implements io.Closer, and must be called when a Value
is discarded. It guarantees a sane exit from the substrate
and returns the value to the value pool.
*/
func (value *Value) Close() error {
	if value == nil {
		return nil
	}

	// Wipe the Value, this is important to ensure 
	// that the Value is not leaked to the heap.
	*value = Value{}
	valuePool.Put(value)

	return nil
}

/*
ID returns the ID of the Value.
*/
func (value *Value) ID() uint64 {
	return value.ID()
}

/*
String returns the string representation of the
Value's token region, which stores the original
bytes of the data that was used to create the Value.
*/
func (value *Value) String() string {
	return string(
		unsafe.Slice(
			(*byte)(unsafe.Pointer(&value[0])),
			core.Cfg.Value.Region.Tokens.Bits,
		),
	)
}
