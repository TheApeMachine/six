package primitive

import (
	"bytes"

	capnp "capnproto.org/go/capnp/v3"
)

func (value Value) Read(p []byte) (n int, err error) {
	encoder := capnp.NewEncoder(bytes.NewBuffer(p))
	err = encoder.Encode(value.Message())

	if err != nil {
		return 0, err
	}

	return 0, nil
}

func (value Value) Write(p []byte) (n int, err error) {
	decoder := capnp.NewDecoder(bytes.NewBuffer(p))
	msg, err := decoder.Decode()
	
	if err != nil {
		return 0, err
	}

	value, err = ReadRootValue(msg)

	if err != nil {
		return 0, err
	}

	return 0, nil
}

func (value Value) Close() error {
	return nil
}
