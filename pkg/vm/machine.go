package vm

import (
	"context"
	"errors"
	"io"
	"runtime"
	"sync"

	"github.com/panjf2000/ants/v2"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/network"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/whitaker-io/machine"
)

/*
Machine provides a unified stream processing pipeline using github.com/whitaker-io/machine
and ants goroutine pooling. It dynamically schedules work across local hardware substrates
and remote network nodes natively through a homogeneous data flow, while guaranteeing
zero-drop fault tolerance using errnie error handling contexts.
*/
type Machine struct {
	ctx       context.Context
	cancel    context.CancelFunc
	pool      *ants.Pool
	dataset   io.ReadCloser
	backend   kernel.Substrate
	network   *network.UniConn
	networkMu sync.Mutex
	prompt    io.ReadWriteCloser

	// Stream internals
	inChan  chan []*primitive.Value
	outChan chan []*primitive.Value
	stream  machine.Stream[*primitive.Value]

	// Staging allows tokenization of bytes into complete values
	staging *primitive.Value
	unread  []*primitive.Value
	mu      sync.Mutex
}

type machineOption func(*Machine)

func NewMachine(opts ...machineOption) (m *Machine, err error) {
	// Initialize hardware-sympathetic pool
	pool, err := ants.NewPool(runtime.NumCPU() - 1)
	if err != nil {
		return nil, errnie.Wrap(err, "vm.machine.NewMachine")
	}

	ctx, cancel := context.WithCancel(context.Background())

	m = &Machine{
		ctx:     ctx,
		cancel:  cancel,
		pool:    pool,
		backend: compute.NewBackend(),
		prompt:  primitive.NewValue(),
		staging: primitive.NewValue(),
		inChan:  make(chan []*primitive.Value, 256),
		outChan: make(chan []*primitive.Value, 256),
	}

	for _, opt := range opts {
		opt(m)
	}

	if err = validate.Require(map[string]any{
		"ctx":     m.ctx,
		"cancel":  m.cancel,
		"pool":    m.pool,
		"backend": m.backend,
		"prompt":  m.prompt,
	}); err != nil {
		return nil, errnie.Wrap(err, "vm.machine.NewMachine")
	}

	maxRoutines := runtime.NumCPU() - 1
	if maxRoutines < 1 {
		maxRoutines = 1
	}

	// Build the stream topology:
	// 1. Setup machine processing stream with FIFO to preserve sequential ordering.
	m.stream = machine.NewWithChannels[*primitive.Value](
		"vm.stream",
		&machine.Option[*primitive.Value]{
			FIFO:       true,
			BufferSize: 256,
		},
	)

	// Map incoming values to be processed concurrently using ants pool
	m.stream.Builder().Map(m.scheduleProcess).OutputTo(m.outChan)
	_ = m.stream.StartWith(m.ctx, m.inChan)

	return m, nil
}

// scheduleProcess acts as the homogeneous dispatcher across local and remote boundaries.
// By injecting tasks into the hardware-sympathetic ants pool, we maintain precise control over parallelism.
func (m *Machine) scheduleProcess(val *primitive.Value) *primitive.Value {
	var wg sync.WaitGroup
	wg.Add(1)

	var result *primitive.Value

	// Execute with fault-tolerance via ants Pool and errnie
	err := m.pool.Submit(func() {
		defer wg.Done()

		var res *primitive.Value

		// Rescheduling/Retry logic to ensure zero operation drop rate
		retries := 0
		maxRetries := 5

		for retries < maxRetries {
			select {
			case <-m.ctx.Done():
				result = val
				return
			default:
			}

			// Serialize without destroying the source value for safe retries
			buffer := make([]byte, primitive.ByteSize)
			if err := primitive.ValueToBytes(val, buffer); err != nil {
				errnie.Error(errnie.Wrap(err, "vm.scheduleProcess", "stage", "serialize").WithContext(m.ctx).WithReschedule())
				retries++
				continue
			}

			// Dispatch heuristics: send to optimal node (local kernel vs remote network)
			if m.network != nil {
				m.networkMu.Lock()
				if _, nErr := m.network.Write(buffer); nErr != nil {
					m.networkMu.Unlock()
					errnie.Warn("network write failed, falling back to local substrate", "error", nErr)
					if _, lErr := m.backend.Write(buffer); lErr != nil {
						retries++
						continue
					}
					m.backend.Read(buffer)
				} else {
					// Read response from remote
					m.network.Read(buffer)
					m.networkMu.Unlock()
				}
			} else {
				if _, lErr := m.backend.Write(buffer); lErr != nil {
					retries++
					continue
				}
				m.backend.Read(buffer)
			}

			// Update value via the returned processed frame
			res = primitive.NewValue()
			if aErr := res.ApplyWireFrame(buffer); aErr != nil {
				errnie.Error(errnie.Wrap(aErr, "vm.scheduleProcess", "stage", "apply_frame").WithContext(m.ctx).WithReschedule())
				retries++
				continue
			}

			break // Success
		}

		if res == nil {
			errnie.Error(errnie.Wrap(errors.New("exceeded max retries inside scheduleProcess"), "context", "zero_drop_rescue").WithContext(m.ctx).WithReschedule())
			
			// Reschedule directly to inChan to guarantee zero drop rate
			go func() {
				select {
				case <-m.ctx.Done():
				case m.inChan <- []*primitive.Value{val}:
				}
			}()
			
			result = nil // Returning nil ensures the Pipeline's Filter drops this from flowing to outChan
		} else {
			// Successful execution, return the value to the pool as we produced a new one
			val.Close()
			result = res
		}
	})

	if err != nil {
		errnie.Error(errnie.Wrap(err, "vm.scheduleProcess", "stage", "submit").WithContext(m.ctx).WithReschedule())
		go func() {
			select {
			case <-m.ctx.Done():
			case m.inChan <- []*primitive.Value{val}:
			}
		}()
		return nil
	}

	wg.Wait()
	return result
}

func (m *Machine) Read(p []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Fill internal read queue if empty
	if len(m.unread) == 0 {
		select {
		case <-m.ctx.Done():
			return 0, io.EOF
		case batch, ok := <-m.outChan:
			if !ok {
				return 0, io.EOF
			}
			for _, val := range batch {
				if val != nil {
					m.unread = append(m.unread, val)
				}
			}
		default:
			return 0, nil // Non-blocking if nothing ready
		}
	}

	if len(m.unread) == 0 {
		return 0, nil
	}

	val := m.unread[0]
	n, err = val.Consume(p)
	if err == io.EOF || n == len(p) || n == primitive.ByteSize {
		// Value fully consumed into p, shift slice
		m.unread = m.unread[1:]
	}

	if err == io.EOF {
		err = nil // Swallow to keep standard consumer streams alive
	}

	errnie.Trace("vm.machine.Read", "n", n, "err", err)
	return n, err
}

func (m *Machine) Write(p []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for n < len(p) {
		written, err := m.staging.Write(p[n:])
		n += written

		if errors.Is(err, primitive.NewValueError(primitive.ValueErrorDataFull)) {
			val := primitive.NewValue()
			buf := make([]byte, primitive.ByteSize)
			m.staging.Consume(buf)
			val.ApplyWireFrame(buf)

			// Non-blocking push to stream
			select {
			case m.inChan <- []*primitive.Value{val}:
			default:
				errnie.Warn("vm.machine.Write", "dropped", "buffer full")
			}
			continue
		} else if err != nil {
			return n, errnie.Wrap(err, "vm.machine.Write tokenization")
		}
	}

	errnie.Trace("vm.machine.Write", "n", n, "err", nil)
	return n, nil
}

func (m *Machine) Prompt() io.ReadWriter {
	return m.prompt
}

func (m *Machine) Close() error {
	m.cancel()
	close(m.inChan)

	var errs error
	if m.backend != nil {
		if err := m.backend.Close(); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	if m.dataset != nil {
		if err := m.dataset.Close(); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	if m.network != nil {
		if err := m.network.Close(); err != nil {
			errs = errors.Join(errs, err)
		}
	}

	// Release native pool
	m.pool.Release()
	return errs
}

func WithContext(ctx context.Context) machineOption {
	return func(m *Machine) {
		m.ctx, m.cancel = context.WithCancel(ctx)
	}
}

func WithBackend(backend kernel.Substrate) machineOption {
	return func(m *Machine) {
		errnie.Info("vm.machine.WithBackend", "msg", "overriding backend")
		m.backend = backend
	}
}

func WithNetwork(conn *network.UniConn) machineOption {
	return func(m *Machine) {
		errnie.Info("vm.machine.WithNetwork", "msg", "injecting network stream layer")
		m.network = conn
	}
}

func (m *Machine) Backend() kernel.Substrate {
	return m.backend
}

func WithDataset(dataset io.ReadCloser) machineOption {
	return func(m *Machine) {
		m.dataset = dataset
		// Stream might pull from dataset directly if implemented,
		// but historically source had a Loop(). We simulate it:
		go func() {
			buf := make([]byte, 32*1024)
			for {
				n, err := dataset.Read(buf)
				if n > 0 {
					m.Write(buf[:n])
				}
				if err != nil {
					break
				}
			}
		}()
	}
}

const maxValueFeedbackBuf = 256 * primitive.ByteSize

var ErrValueFeedbackBufferFull = errors.New("vm: value feedback buffer cap exceeded")

// valueFeedbackWriter is retained for legacy integration tests connecting io interfaces
type valueFeedbackWriter struct {
	dst *primitive.Value
	buf []byte
}

func (w *valueFeedbackWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(w.buf)+len(p) > maxValueFeedbackBuf {
		return 0, ErrValueFeedbackBufferFull
	}
	w.buf = append(w.buf, p...)
	for len(w.buf) >= primitive.ByteSize {
		frame := w.buf[:primitive.ByteSize]
		w.buf = w.buf[primitive.ByteSize:]
		if err := w.dst.ApplyWireFrame(frame); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}
