package telemetry

// Emitter is the interface for sending telemetry events. Components accept an
// Emitter so telemetry can be swapped for a no-op when disabled.
type Emitter interface {
	Emit(Event)
	EmitFrame([]byte)
	Close() error
}

// NoopEmitter silently discards all events. It is a zero-size struct so calls
// compile down to nothing on the hot path.
type NoopEmitter struct{}

func (NoopEmitter) Emit(Event)        {}
func (NoopEmitter) EmitFrame([]byte)  {}
func (NoopEmitter) Close() error      { return nil }

var _ Emitter = NoopEmitter{}
