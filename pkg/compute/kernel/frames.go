package kernel

import (
	"unsafe"

	"github.com/theapemachine/six/pkg/core"
)

/*
PackValueFrames copies valueBytes() bytes from each non-nil frame pointer into
one contiguous byte slab row-major, suitable for the Metal/CUDA host buffers.
Each non-nil pointer must reference exactly valueBytes() bytes of valid memory;
current callers pass Value frames backed by [128]uint64. Nil pointers are
skipped without copying.
*/
func PackValueFrames(frames []unsafe.Pointer) []byte {

	vb := core.Cfg.Value.Bytes
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

	vb := core.Cfg.Value.Bytes
	if len(frames) == 0 || len(slab) < vb {
		return
	}

	for index := 0; index < len(frames); index++ {
		framePtr := frames[index]
		if framePtr == nil {
			continue
		}

		src := slab[index*vb : (index+1)*vb]
		copy(unsafe.Slice((*byte)(framePtr), vb), src)
	}
}
