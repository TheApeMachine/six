package telemetry

import "sync/atomic"

// globalSlot wraps Emitter so atomic storage always uses one concrete type.
// atomic.Value (and storing Emitter directly) would panic: first Store was
// NoopEmitter{}, later SetGlobal(*UDPSender) is a different dynamic type.
type globalSlot struct {
	e Emitter
}

var globalEmitter atomic.Pointer[globalSlot]

func init() {
	globalEmitter.Store(&globalSlot{e: NoopEmitter{}})
}

// SetGlobal installs the process-wide Emitter. Pass nil to reset to NoopEmitter.
func SetGlobal(e Emitter) {
	if e == nil {
		e = NoopEmitter{}
	}
	globalEmitter.Store(&globalSlot{e: e})
}

// Global returns the current process-wide Emitter. Never returns nil.
func Global() Emitter {
	return globalEmitter.Load().e
}

// Emit is a convenience wrapper for Global().Emit(ev).
func Emit(ev Event) {
	Global().Emit(ev)
}

// EmitFrame is a convenience wrapper for Global().EmitFrame(frame).
func EmitFrame(frame []byte) {
	Global().EmitFrame(frame)
}
