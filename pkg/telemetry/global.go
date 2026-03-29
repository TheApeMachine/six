package telemetry

import "sync/atomic"

var globalEmitter atomic.Value

func init() {
	globalEmitter.Store(NoopEmitter{})
}

// SetGlobal installs the process-wide Emitter. Pass nil to reset to NoopEmitter.
func SetGlobal(e Emitter) {
	if e == nil {
		e = NoopEmitter{}
	}
	globalEmitter.Store(e)
}

// Global returns the current process-wide Emitter. Never returns nil.
func Global() Emitter {
	return globalEmitter.Load().(Emitter)
}

// Emit is a convenience wrapper for Global().Emit(ev).
func Emit(ev Event) {
	Global().Emit(ev)
}

// EmitFrame is a convenience wrapper for Global().EmitFrame(frame).
func EmitFrame(frame []byte) {
	Global().EmitFrame(frame)
}
