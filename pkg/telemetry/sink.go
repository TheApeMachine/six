package telemetry

import "sync"

/*
wireValueSink forwards raw Value wire images to the active transport.
Tests install a temporary sink to assert frame publication without networking.
*/
var wireValueSink func([]byte)
var wireValueSinkMu sync.Mutex

/*
SetWireValueFrameSink installs a raw frame sink.
Installing a non-nil sink activates the bus so gated publishers emit during tests.
*/
func SetWireValueFrameSink(fn func([]byte)) {
	wireValueSink = fn

	if fn != nil {
		DefaultBus.Activate()
	}
}

/*
PublishWireValueFrame emits one raw Value image if a sink is configured.
*/
func PublishWireValueFrame(valueID uint64, frame []byte) {
	if wireValueSink == nil || len(frame) == 0 {
		return
	}

	payload := MarshalWireValueFrame(valueID, frame)
	wireValueSinkMu.Lock()
	defer wireValueSinkMu.Unlock()
	wireValueSink(payload)
}
