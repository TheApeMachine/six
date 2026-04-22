package transport

import "github.com/theapemachine/six/pkg/primitive"

/*
Publishable receives zero or more live *primitive.Value records from a framed
byte path (Stream reassembly or tokenizer drain) and returns any values the
sink forwards (for example vm.Orchestrator.Cycle).
*/
type Publishable interface {
	Publish(values ...*primitive.Value) ([]*primitive.Value, error)
}

/*
LabelingPublishable is optional for sinks that still need a logical label on
tokenizer ingest (pool.Queue task routing) while sharing the same variadic
Publish contract everywhere else.
*/
type LabelingPublishable interface {
	PublishLabeled(label string, values ...*primitive.Value) ([]*primitive.Value, error)
}

