/*
Package primitive includes a contiguous Value arena: each Value is 128×uint64
(1024 bytes). Default arena capacity is set at process start from
SIX_VALUE_ARENA_SLOTS (decimal); if unset, defaultArenaSlotsFallback applies
(~64 MiB for the slab). Raise the env var when workloads need more in-arena
slots; lower it on memory-constrained hosts.
*/
package primitive

import (
	"errors"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"unsafe"
)

/*
ErrNotArenaValue is returned when a pointer does not refer to a slot inside
the contiguous valueArena slab (e.g. heap fallback Values).
*/
var ErrNotArenaValue = errors.New("primitive: Value pointer not in arena slab")

var heapValuePool = sync.Pool{
	New: func() any {
		return new(Value)
	},
}

const defaultArenaSlotsFallback = 1 << 16

/*
ArenaSlotCount is the number of Value slots in the contiguous arena slab.
It is initialized from SIX_VALUE_ARENA_SLOTS or defaultArenaSlotsFallback.
*/
var ArenaSlotCount int

func init() {
	ArenaSlotCount = defaultArenaSlotsFallback

	raw := os.Getenv("SIX_VALUE_ARENA_SLOTS")
	if raw == "" {
		return
	}

	parsed, parseErr := strconv.ParseUint(raw, 10, 31)
	if parseErr != nil || parsed == 0 {
		return
	}

	ArenaSlotCount = int(parsed)
}

var (
	valueArena     []Value
	valueArenaOnce sync.Once
	arenaMutex     sync.Mutex
	freeArenaIdx   []uint32

	// arenaLinearNext is the next slot index for lock-free linear allocation
	// when the free list is empty. It is shared with GPU atomics when the
	// device path registers the address (cudaHostRegister / Metal no-copy).
	arenaLinearNext uint32

	arenaPinOnce sync.Once
	arenaPinner  runtime.Pinner

	heapFallbackAllocations atomic.Uint64
)

func ensureArena() {
	valueArenaOnce.Do(func() {
		valueArena = make([]Value, ArenaSlotCount)
		freeArenaIdx = make([]uint32, 0, 1024)
	})
}

/*
EnsureArenaPinnedForGPU pins the arena backing store and the linear allocation
counter so device kernels may alias the same physical pages. Idempotent.
*/
func EnsureArenaPinnedForGPU() {
	ensureArena()

	arenaPinOnce.Do(func() {
		arenaPinner.Pin(unsafe.SliceData(valueArena))
		arenaPinner.Pin(unsafe.Pointer(&arenaLinearNext))
	})
}

/*
ArenaLinearNextPtr returns the address of the shared linear bump counter for
device registration. The counter uses the same atomic operations as
AllocValue's bump path when linked with GPU code.
*/
func ArenaLinearNextPtr() *uint32 {
	return &arenaLinearNext
}

/*
ArenaBasePointer returns the base address and byte length of the contiguous
arena for one-time device mapping.
*/
func ArenaBasePointer() (base unsafe.Pointer, byteLen uintptr) {
	ensureArena()

	if len(valueArena) == 0 {
		return nil, 0
	}

	return unsafe.Pointer(&valueArena[0]), uintptr(len(valueArena)) * unsafe.Sizeof(Value{})
}

/*
ArenaIndex returns the arena slot index for a Value allocated inside the slab.
*/
func ArenaIndex(value *Value) (uint32, bool) {
	if value == nil {
		return 0, false
	}

	ensureArena()

	if len(valueArena) == 0 {
		return 0, false
	}

	base := uintptr(unsafe.Pointer(&valueArena[0]))
	ptr := uintptr(unsafe.Pointer(value))
	step := unsafe.Sizeof(Value{})
	end := base + uintptr(len(valueArena))*step

	if ptr < base || ptr >= end {
		return 0, false
	}

	return uint32((ptr - base) / step), true
}

/*
ValueAt returns the Value at the given arena slot or nil if out of range.
*/
func ValueAt(slot uint32) *Value {
	ensureArena()

	if int(slot) >= len(valueArena) {
		return nil
	}

	return &valueArena[slot]
}

/*
IndicesFromPointers maps host pointers into arena slot indices. Any pointer
not backed by the slab returns ErrNotArenaValue.
*/
func IndicesFromPointers(ptrs []unsafe.Pointer) ([]uint32, error) {
	if len(ptrs) == 0 {
		return nil, nil
	}

	out := make([]uint32, 0, len(ptrs))

	for _, ptr := range ptrs {
		if ptr == nil {
			continue
		}

		idx, ok := ArenaIndex((*Value)(ptr))
		if !ok {
			return nil, ErrNotArenaValue
		}

		out = append(out, idx)
	}

	return out, nil
}

/*
AllocValue returns a zeroed Value from the contiguous arena when possible,
otherwise allocates a fresh heap Value (same shape as legacy sync.Pool).
*/
func AllocValue() *Value {
	ensureArena()

	arenaMutex.Lock()

	if freeListLen := len(freeArenaIdx); freeListLen > 0 {
		slot := freeArenaIdx[freeListLen-1]
		freeArenaIdx = freeArenaIdx[:freeListLen-1]
		arenaMutex.Unlock()

		value := &valueArena[slot]
		*value = Value{}

		return value
	}

	arenaMutex.Unlock()

	for {
		slot := atomic.LoadUint32(&arenaLinearNext)
		if int(slot) >= len(valueArena) {
			break
		}

		if atomic.CompareAndSwapUint32(&arenaLinearNext, slot, slot+1) {
			value := &valueArena[slot]
			*value = Value{}

			return value
		}
	}

	value := heapValuePool.Get().(*Value)
	*value = Value{}
	heapFallbackAllocations.Add(1)

	return value
}

/*
HeapFallbackAllocations reports how many times AllocValue had to spill out of
the contiguous arena and source a Value from the heap pool instead.
*/
func HeapFallbackAllocations() uint64 {
	return heapFallbackAllocations.Load()
}

/*
FreeValue returns a Value to the arena or the heap pool.
*/
func FreeValue(value *Value) {
	if value == nil {
		return
	}

	*value = Value{}

	ensureArena()

	if len(valueArena) == 0 {
		heapValuePool.Put(value)

		return
	}

	base := uintptr(unsafe.Pointer(&valueArena[0]))
	ptr := uintptr(unsafe.Pointer(value))
	end := base + uintptr(len(valueArena))*unsafe.Sizeof(Value{})

	if ptr < base || ptr >= end {
		heapValuePool.Put(value)

		return
	}

	slot := uint32((ptr - base) / unsafe.Sizeof(Value{}))

	arenaMutex.Lock()
	freeArenaIdx = append(freeArenaIdx, slot)
	arenaMutex.Unlock()
}
