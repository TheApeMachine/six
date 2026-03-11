package data

import (
	"github.com/google/uuid"
	"github.com/theapemachine/six/pool"
)

type Receiver uint

const (
	SPATIALINDEX Receiver = iota
	TOKENIZER
)

type MessageType uint

const (
	REQUEST MessageType = iota
	RESPONSE
)

type Message struct {
	ID       uuid.UUID
	Parent   uuid.UUID
	Receiver Receiver
	Type     MessageType
	Value    pool.PoolValue[any]
}
