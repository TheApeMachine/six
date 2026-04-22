package kernel

import (
	"sync"
	"sync/atomic"
)

// Observer receives optional kernel-level trace/error signals.
// Implementations must return quickly and should avoid blocking hot paths.
type Observer interface {
	Trace(event string, keyvals ...any)
	Error(event string, err error, keyvals ...any)
}

// ObserverAware is implemented by backends that support observer injection.
type ObserverAware interface {
	SetObserver(observer Observer)
}

// NoopObserver drops all events.
type NoopObserver struct{}

func (NoopObserver) Trace(string, ...any)        {}
func (NoopObserver) Error(string, error, ...any) {}

// ObserverFuncs adapts plain functions to Observer.
type ObserverFuncs struct {
	TraceFn func(event string, keyvals ...any)
	ErrorFn func(event string, err error, keyvals ...any)
}

func (f ObserverFuncs) Trace(event string, keyvals ...any) {
	if f.TraceFn != nil {
		f.TraceFn(event, keyvals...)
	}
}

func (f ObserverFuncs) Error(event string, err error, keyvals ...any) {
	if f.ErrorFn != nil {
		f.ErrorFn(event, err, keyvals...)
	}
}

// NormalizeObserver guarantees a non-nil observer.
func NormalizeObserver(observer Observer) Observer {
	if observer == nil {
		return NoopObserver{}
	}
	return observer
}

type observerEventType uint8

const (
	observerEventTrace observerEventType = iota
	observerEventError
)

type observerEvent struct {
	kind    observerEventType
	event   string
	err     error
	keyvals []any
}

// AsyncObserver wraps another observer with a non-blocking queue.
// When the queue is full, events are dropped.
type AsyncObserver struct {
	target  Observer
	queue   chan observerEvent
	dropped atomic.Uint64
	done    chan struct{}
	once    sync.Once
}

// NewAsyncObserver returns a fire-and-forget observer wrapper.
func NewAsyncObserver(target Observer, queueSize int) *AsyncObserver {
	if queueSize <= 0 {
		queueSize = 256
	}
	o := &AsyncObserver{
		target: NormalizeObserver(target),
		queue:  make(chan observerEvent, queueSize),
		done:   make(chan struct{}),
	}
	go o.run()
	return o
}

func (o *AsyncObserver) run() {
	defer close(o.done)
	for ev := range o.queue {
		switch ev.kind {
		case observerEventTrace:
			o.target.Trace(ev.event, ev.keyvals...)
		case observerEventError:
			o.target.Error(ev.event, ev.err, ev.keyvals...)
		}
	}
}

func (o *AsyncObserver) enqueue(ev observerEvent) {
	if o == nil {
		return
	}
	select {
	case o.queue <- ev:
	default:
		o.dropped.Add(1)
	}
}

func (o *AsyncObserver) Trace(event string, keyvals ...any) {
	if o == nil {
		return
	}
	copied := append([]any(nil), keyvals...)
	o.enqueue(observerEvent{
		kind:    observerEventTrace,
		event:   event,
		keyvals: copied,
	})
}

func (o *AsyncObserver) Error(event string, err error, keyvals ...any) {
	if o == nil || err == nil {
		return
	}
	copied := append([]any(nil), keyvals...)
	o.enqueue(observerEvent{
		kind:    observerEventError,
		event:   event,
		err:     err,
		keyvals: copied,
	})
}

// Dropped returns the number of events dropped due to queue pressure.
func (o *AsyncObserver) Dropped() uint64 {
	if o == nil {
		return 0
	}
	return o.dropped.Load()
}

// Close flushes queued events and stops the worker.
func (o *AsyncObserver) Close() {
	if o == nil {
		return
	}
	o.once.Do(func() {
		close(o.queue)
		<-o.done
	})
}

