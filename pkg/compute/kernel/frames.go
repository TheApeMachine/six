package kernel

import (
	"unsafe"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
valueBytes returns the packed host size of one Value frame for kernel I/O.
Config wins when loaded; otherwise we use the primitive default (1024).
*/
func valueBytes() int {

	if core.Cfg != nil && core.Cfg.Value.Bytes > 0 {
		return core.Cfg.Value.Bytes
	}

	return primitive.ByteSize
}

/*
PackValueFrames copies each host frame into one contiguous byte slab row-major
(1024-byte rows by default), suitable for the Metal/CUDA batch host buffers.
Nil pointers are skipped without copying; callers should reject nils before packing.
*/
func PackValueFrames(frames []unsafe.Pointer) []byte {

	vb := valueBytes()
	out := make([]byte, len(frames)*vb)

	for index, framePtr := range frames {
		if framePtr == nil {
			continue
		}

		dst := out[index*vb : (index+1)*vb]
		copy(dst, unsafe.Slice((*byte)(framePtr), vb))
	}

	return out
}

/*
UnpackValueFrames writes each row of slab back to the corresponding frame pointer.
*/
func UnpackValueFrames(frames []unsafe.Pointer, slab []byte) {

	vb := valueBytes()

	for index, framePtr := range frames {
		if framePtr == nil {
			continue
		}

		src := slab[index*vb : (index+1)*vb]
		copy(unsafe.Slice((*byte)(framePtr), vb), src)
	}
}
