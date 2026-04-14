package viz

/*
wireValueSink delivers raw Value.Bytes frames (see MarshalWireValueFrame) to
the viz HTTP server. Registered from Server.ListenAndActivate; nil means
PublishWireValueFrame is a no-op so library tests never need a server.
*/
var wireValueSink func(payload []byte)

/*
SetWireValueFrameSink registers the callback that broadcasts marshaled
WireFrameValue blobs to WebSocket clients. Only the viz Server should call this.
*/
func SetWireValueFrameSink(fn func([]byte)) {
	wireValueSink = fn
}

/*
PublishWireValueFrame sends one full primitive.Value wire image after optional
instrumentation hooks have stamped program / affinity / properties. The sink
copies the byte slice before returning.
*/
func PublishWireValueFrame(valueID uint64, frame []byte) {
	if wireValueSink == nil || len(frame) == 0 {
		return
	}

	payload := MarshalWireValueFrame(valueID, frame)
	wireValueSink(payload)
}
