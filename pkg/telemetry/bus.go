package telemetry

import (
	"maps"
	"sync"
	"sync/atomic"
)

type subscriber struct {
	ch     chan Event
	filter func(Event) bool
}

/*
Bus is a non-blocking fan-out bus for telemetry events.
Publish is a cheap no-op when the bus is inactive.
*/
type Bus struct {
	mu          sync.RWMutex
	subscribers []subscriber
	active      atomic.Bool
	dropped     atomic.Uint64
}

/*
NewBus creates a dormant telemetry bus.
*/
func NewBus() *Bus {
	return &Bus{}
}

/*
Activate enables event publishing.
*/
func (bus *Bus) Activate() {
	bus.active.Store(true)
}

/*
Deactivate disables event publishing.
*/
func (bus *Bus) Deactivate() {
	bus.active.Store(false)
}

/*
IsActive reports whether the bus is accepting events.
*/
func (bus *Bus) IsActive() bool {
	return bus.active.Load()
}

/*
Dropped reports the number of events dropped because a subscriber was full.
*/
func (bus *Bus) Dropped() uint64 {
	return bus.dropped.Load()
}

/*
Subscribe registers a buffered event consumer.
*/
func (bus *Bus) Subscribe(bufSize int, filter func(Event) bool) chan Event {
	if bufSize <= 0 {
		bufSize = 4096
	}

	channel := make(chan Event, bufSize)

	bus.mu.Lock()
	bus.subscribers = append(bus.subscribers, subscriber{ch: channel, filter: filter})
	bus.mu.Unlock()

	return channel
}

/*
Unsubscribe removes and closes a previously registered channel.
*/
func (bus *Bus) Unsubscribe(channel chan Event) {
	bus.mu.Lock()
	defer bus.mu.Unlock()

	for index, item := range bus.subscribers {
		if item.ch != channel {
			continue
		}

		bus.subscribers = append(bus.subscribers[:index], bus.subscribers[index+1:]...)
		close(channel)

		return
	}
}

/*
Publish fans an event out to every subscriber without blocking producers.
*/
func (bus *Bus) Publish(event Event) {
	if !bus.active.Load() {
		return
	}

	event = freezeEvent(event)

	bus.mu.RLock()
	subscribers := bus.subscribers
	bus.mu.RUnlock()

	for _, item := range subscribers {
		if item.filter != nil && !item.filter(event) {
			continue
		}

		select {
		case item.ch <- event:
		default:
			bus.dropped.Add(1)
		}
	}
}

func cloneString(value string) string {
	if value == "" {
		return ""
	}

	return string(append([]byte(nil), value...))
}

func freezeEvent(event Event) Event {
	event.Source = cloneString(event.Source)
	event.Target = cloneString(event.Target)
	event.Label = cloneString(event.Label)

	if len(event.Values) == 0 {
		event.Values = nil
	} else {
		event.Values = maps.Clone(event.Values)
	}

	if len(event.Meta) == 0 {
		event.Meta = nil
		return event
	}

	frozenMeta := make(map[string]string, len(event.Meta))

	for key, value := range event.Meta {
		frozenMeta[cloneString(key)] = cloneString(value)
	}

	event.Meta = frozenMeta

	return event
}

/*
DefaultBus is the process-wide telemetry bus used by runtime publishers.
*/
var DefaultBus = NewBus()
