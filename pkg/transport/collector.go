package transport

import "io"

/*
Collector is a growable byte buffer used as an io.Writer destination for
io.Copy when *bytes.Buffer must be avoided: *bytes.Buffer implements
io.ReaderFrom, so io.Copy delegates to ReadFrom and the source may see
small first reads (bytes.MinRead), which breaks framed readers that require
each Read(p) to use at least one full wire frame (e.g. gossip.Conn).

Collector only implements Write (not ReaderFrom), so io.Copy uses the
stdlib copy loop with a large internal buffer. Len and Next mirror
bytes.Buffer semantics for pending byte count and consuming the next n
bytes from the front.
*/
type Collector struct {
	buf []byte
}

// NewCollector returns an empty collector; initialCap hints preallocation
// (e.g. core.Cfg.Value.Bytes).
func NewCollector(initialCap int) *Collector {
	if initialCap < 0 {
		initialCap = 0
	}

	return &Collector{
		buf: make([]byte, 0, initialCap),
	}
}

// Len returns the number of pending bytes (like bytes.Buffer.Len).
func (collector *Collector) Len() int {
	if collector == nil {
		return 0
	}

	return len(collector.buf)
}

// Next returns the next n bytes and removes them from the front. If fewer
// than n bytes are buffered, it returns nil.
func (collector *Collector) Next(n int) []byte {
	if collector == nil || n <= 0 || len(collector.buf) < n {
		return nil
	}

	frame := collector.buf[:n]
	collector.buf = collector.buf[n:]

	return frame
}

// Read consumes up to len(p) bytes from the front of the buffer.
func (collector *Collector) Read(p []byte) (n int, err error) {
	if collector == nil {
		return 0, io.ErrClosedPipe
	}

	if len(p) == 0 {
		return 0, nil
	}

	if len(collector.buf) == 0 {
		return 0, io.EOF
	}

	n = copy(p, collector.buf)
	collector.buf = collector.buf[n:]

	return n, nil
}

// Write appends a copy of p so io.Copy's reused scratch buffer cannot alias
// stored data.
func (collector *Collector) Write(p []byte) (n int, err error) {
	if collector == nil {
		return 0, io.ErrClosedPipe
	}

	collector.buf = append(collector.buf, p...)

	return len(p), nil
}
