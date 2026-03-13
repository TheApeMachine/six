package data

import (
	"github.com/google/uuid"
	"github.com/theapemachine/six/pkg/pool"
)

/*
Receiver identifies the intended recipient subsystem for a broadcast message.
*/
type Receiver uint

const (
	SPATIALINDEX Receiver = iota
	TOKENIZER
)

/*
MessageType distinguishes between inbound requests and outbound responses.
*/
type MessageType uint

const (
	REQUEST MessageType = iota
	RESPONSE
)

/*
Message encapsulates a generic broadcast payload with correlation IDs
and routing metadata for the decentralized event bus.
*/
type Message struct {
	ID       uuid.UUID
	Parent   uuid.UUID
	Receiver Receiver
	Type     MessageType
	Value    pool.PoolValue[any]
}
