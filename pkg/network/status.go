package network

import (
	"io"
	"sync"
	"time"
)

/*
TransportTopology describes where a transport is expected to operate.
*/
type TransportTopology string

const (
	TransportTopologyUnknown     TransportTopology = "unknown"
	TransportTopologySameMachine TransportTopology = "same-machine"
	TransportTopologyLAN         TransportTopology = "lan"
	TransportTopologyWAN         TransportTopology = "wan"
)

/*
TransportFailureMode classifies the dominant reason a transport failed.
*/
type TransportFailureMode string

const (
	TransportFailureNone         TransportFailureMode = "none"
	TransportFailureNotReady     TransportFailureMode = "not-ready"
	TransportFailureDependency   TransportFailureMode = "dependency"
	TransportFailureBind         TransportFailureMode = "bind"
	TransportFailureDial         TransportFailureMode = "dial"
	TransportFailureHandshake    TransportFailureMode = "handshake"
	TransportFailureTimeout      TransportFailureMode = "timeout"
	TransportFailureBackpressure TransportFailureMode = "backpressure"
	TransportFailureProtocol     TransportFailureMode = "protocol"
	TransportFailureClosed       TransportFailureMode = "closed"
	TransportFailureCanceled     TransportFailureMode = "canceled"
	TransportFailureCircuitOpen  TransportFailureMode = "circuit-open"
	TransportFailureUnknown      TransportFailureMode = "unknown"
)

/*
CircuitState reports the state of a transport's breaker.
*/
type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"
	CircuitOpen     CircuitState = "open"
	CircuitHalfOpen CircuitState = "half-open"
)

/*
TransportTraits captures the semantic properties of one transport.
*/
type TransportTraits struct {
	Name            string
	Topology        TransportTopology
	Reliable        bool
	Ordered         bool
	MessageOriented bool
	Broadcast       bool
	Encrypted       bool
	ExternalRuntime bool
}

/*
TransportStatus is the observable health snapshot of one transport instance.
*/
type TransportStatus struct {
	Ready               bool
	Degraded            bool
	Breaker             CircuitState
	SystemicFailure     bool
	ConsecutiveFailures int
	TotalSuccesses      uint64
	TotalFailures       uint64
	LastFailureMode     TransportFailureMode
	LastFailure         error
	LastFailureAt       time.Time
	LastSuccessAt       time.Time
	NextAttemptAt       time.Time
}

/*
ManagedTransport extends io.ReadWriteCloser with trait and health reporting.
*/
type ManagedTransport interface {
	io.ReadWriteCloser
	ReadyTransport
	Traits() TransportTraits
	Status() TransportStatus
}

/*
CircuitBreaker protects a transport from repeated failing calls.
*/
type CircuitBreaker struct {
	failureThreshold int
	cooldown         time.Duration
	state            CircuitState
	consecutive      int
	nextAttemptAt    time.Time
}

/*
breakerOption configures a CircuitBreaker.
*/
type breakerOption func(*CircuitBreaker)

/*
NewCircuitBreaker constructs a breaker with conservative defaults.
*/
func NewCircuitBreaker(options ...breakerOption) *CircuitBreaker {
	breaker := &CircuitBreaker{
		failureThreshold: 3,
		cooldown:         time.Second,
		state:            CircuitClosed,
	}

	for _, option := range options {
		option(breaker)
	}

	if breaker.failureThreshold <= 0 {
		breaker.failureThreshold = 1
	}

	if breaker.cooldown <= 0 {
		breaker.cooldown = time.Second
	}

	return breaker
}

/*
BreakerWithFailureThreshold overrides how many failures are tolerated before
opening the circuit.
*/
func BreakerWithFailureThreshold(threshold int) breakerOption {
	return func(breaker *CircuitBreaker) {
		breaker.failureThreshold = threshold
	}
}

/*
BreakerWithCooldown overrides how long the breaker stays open before probing.
*/
func BreakerWithCooldown(cooldown time.Duration) breakerOption {
	return func(breaker *CircuitBreaker) {
		breaker.cooldown = cooldown
	}
}

func (breaker *CircuitBreaker) Allow(now time.Time) bool {
	if breaker.state != CircuitOpen {
		return true
	}

	if now.Before(breaker.nextAttemptAt) {
		return false
	}

	breaker.state = CircuitHalfOpen
	return true
}

func (breaker *CircuitBreaker) RecordSuccess(now time.Time) {
	breaker.state = CircuitClosed
	breaker.consecutive = 0
	breaker.nextAttemptAt = time.Time{}
}

func (breaker *CircuitBreaker) RecordFailure(now time.Time, systemic bool) {
	breaker.consecutive++

	threshold := breaker.failureThreshold
	if systemic && threshold > 1 {
		threshold = 1
	}

	if breaker.consecutive < threshold {
		return
	}

	breaker.state = CircuitOpen
	breaker.nextAttemptAt = now.Add(breaker.cooldown)
}

func (breaker *CircuitBreaker) State() CircuitState {
	return breaker.state
}

func (breaker *CircuitBreaker) NextAttemptAt() time.Time {
	return breaker.nextAttemptAt
}

func (breaker *CircuitBreaker) ConsecutiveFailures() int {
	return breaker.consecutive
}

/*
transportMonitor keeps shared health and breaker state for one transport.
*/
type transportMonitor struct {
	traits  TransportTraits
	breaker *CircuitBreaker
	status  TransportStatus
	mutex   sync.RWMutex
}

/*
monitorOption configures a transport monitor.
*/
type monitorOption func(*transportMonitor)

/*
newTransportMonitor constructs a monitor for one concrete transport.
*/
func newTransportMonitor(traits TransportTraits, options ...monitorOption) *transportMonitor {
	monitor := &transportMonitor{
		traits:  traits,
		breaker: NewCircuitBreaker(),
		status: TransportStatus{
			Breaker:         CircuitClosed,
			LastFailureMode: TransportFailureNone,
		},
	}

	for _, option := range options {
		option(monitor)
	}

	if monitor.breaker == nil {
		monitor.breaker = NewCircuitBreaker()
	}

	return monitor
}

/*
MonitorWithBreaker installs a custom breaker for the transport.
*/
func MonitorWithBreaker(breaker *CircuitBreaker) monitorOption {
	return func(monitor *transportMonitor) {
		monitor.breaker = breaker
	}
}

func (monitor *transportMonitor) Traits() TransportTraits {
	return monitor.traits
}

func (monitor *transportMonitor) Status() TransportStatus {
	monitor.mutex.RLock()
	defer monitor.mutex.RUnlock()

	return monitor.status
}

func (monitor *transportMonitor) Allow(layer string, op string) error {
	monitor.mutex.Lock()
	defer monitor.mutex.Unlock()

	now := time.Now()
	if monitor.breaker.Allow(now) {
		monitor.status.Breaker = monitor.breaker.State()
		monitor.status.NextAttemptAt = monitor.breaker.NextAttemptAt()
		return nil
	}

	monitor.status.Degraded = true
	monitor.status.Ready = false
	monitor.status.Breaker = monitor.breaker.State()
	monitor.status.NextAttemptAt = monitor.breaker.NextAttemptAt()
	monitor.status.LastFailureMode = TransportFailureCircuitOpen
	monitor.status.LastFailure = ErrTransportCircuitOpen
	monitor.status.LastFailureAt = now
	monitor.status.SystemicFailure = true

	return &NetworkError{
		Subsystem:    layer,
		Op:       op,
		Mode:     TransportFailureCircuitOpen,
		Systemic: true,
		Err:      ErrTransportCircuitOpen,
	}
}

func (monitor *transportMonitor) RecordReady() {
	monitor.mutex.Lock()
	defer monitor.mutex.Unlock()

	monitor.status.Ready = true
	monitor.status.Breaker = monitor.breaker.State()
	monitor.status.NextAttemptAt = monitor.breaker.NextAttemptAt()
}

func (monitor *transportMonitor) RecordSuccess() {
	monitor.mutex.Lock()
	defer monitor.mutex.Unlock()

	now := time.Now()
	monitor.breaker.RecordSuccess(now)
	monitor.status.Ready = true
	monitor.status.Degraded = false
	monitor.status.SystemicFailure = false
	monitor.status.TotalSuccesses++
	monitor.status.LastSuccessAt = now
	monitor.status.Breaker = monitor.breaker.State()
	monitor.status.ConsecutiveFailures = monitor.breaker.ConsecutiveFailures()
	monitor.status.NextAttemptAt = monitor.breaker.NextAttemptAt()
}

func (monitor *transportMonitor) RecordFailure(mode TransportFailureMode, err error, systemic bool) {
	if err == nil {
		return
	}

	monitor.mutex.Lock()
	defer monitor.mutex.Unlock()

	now := time.Now()
	monitor.breaker.RecordFailure(now, systemic)
	monitor.status.Degraded = true
	if systemic {
		monitor.status.Ready = false
	}
	monitor.status.SystemicFailure = systemic
	monitor.status.TotalFailures++
	monitor.status.LastFailureMode = mode
	monitor.status.LastFailure = err
	monitor.status.LastFailureAt = now
	monitor.status.Breaker = monitor.breaker.State()
	monitor.status.ConsecutiveFailures = monitor.breaker.ConsecutiveFailures()
	monitor.status.NextAttemptAt = monitor.breaker.NextAttemptAt()
}
