package telemetry

import "sync"

/*
wireValueSink forwards raw Value wire images to the active transport.
Tests install a temporary sink to assert frame publication without networking.
*/
var wireValueSink func([]byte)
var wireValueSinkMu sync.RWMutex

/*
SetWireValueFrameSink installs a raw frame sink.
*/
func SetWireValueFrameSink(fn func([]byte)) {
	wireValueSinkMu.Lock()
	wireValueSink = fn
	wireValueSinkMu.Unlock()
}

/*
HasWireValueFrameSink reports whether a raw wire sink is installed.
*/
func HasWireValueFrameSink() bool {
	wireValueSinkMu.RLock()
	defer wireValueSinkMu.RUnlock()

	return wireValueSink != nil
}

/*
PublishWireValueFrame emits one raw Value image if a sink is configured.
*/
func PublishWireValueFrame(valueID uint64, frame []byte) {
	if len(frame) == 0 {
		return
	}

	wireValueSinkMu.RLock()
	sink := wireValueSink
	wireValueSinkMu.RUnlock()

	if sink == nil {
		return
	}

	payload := MarshalWireValueFrame(valueID, frame)
	sink(payload)
}
