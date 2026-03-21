package primitive

import (
	"bytes"
	"fmt"

	capnp "capnproto.org/go/capnp/v3"
)

/*
Read serializes the Value shell into p as one Cap'n Proto message snapshot.
*/
func (value *Value) Read(p []byte) (n int, err error) {
	if value == nil || !value.IsValid() {
		return 0, fmt.Errorf("primitive/value io read: invalid receiver")
	}

	var buffer bytes.Buffer

	msg, err := value.snapshotMessage()
	if err != nil {
		return 0, err
	}

	err = capnp.NewEncoder(&buffer).Encode(msg)

	if err != nil {
		return 0, err
	}

	return copy(p, buffer.Bytes()), nil
}

/*
Write decodes one Cap'n Proto Value message into the receiver shell.
*/
func (value *Value) Write(p []byte) (n int, err error) {
	if value == nil {
		return 0, fmt.Errorf("primitive/value io write: nil receiver")
	}

	decoder := capnp.NewDecoder(bytes.NewReader(p))
	msg, err := decoder.Decode()

	if err != nil {
		return 0, err
	}

	incoming, err := ReadRootValue(msg)

	if err != nil {
		return 0, err
	}

	value.CopyFrom(incoming)

	return len(p), nil
}

/*
snapshotMessage lifts the current Value into a fresh root message for io transport.
*/
func (value *Value) snapshotMessage() (*capnp.Message, error) {
	msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		return nil, err
	}

	root, err := NewRootValue(seg)
	if err != nil {
		return nil, err
	}

	root.CopyFrom(*value)

	return msg, nil
}

/*
Close implements io.Closer for Value.
*/
func (value *Value) Close() error {
	return nil
}
