package transport

import (
	"fmt"
	"io"
)

/*
NewFlipFlop creates a synchronous round-trip between two ReadWriters:
io.Copy(to, from) then io.Copy(from, to) in the calling goroutine.

It is intentionally not concurrent. Scheduling extra goroutines here would
steal work from callers that already own a pool or runtime budget. If you
need asynchronous bidirectional buffering, use Stream (ring-buffer pipe)
instead.

This is most commonly used when you want to flip an artifact into
another process, which will set some state on that artifact, that
will be accessible once the process flops the artifact.

Parameters:
  - from: Source ReadWriter to read initial data from and write response to
  - to: Destination ReadWriter to write data to and read response from

Returns:
  - error: Any error that occurred during the data exchange

Example:

```go

	package main

	type Setter struct {
		buffer *stream.Buffer
	}

	func NewSetter() *Setter {
		return &Setter{
			buffer: stream.NewBuffer(func(artifact *datura.Artifact) (err error) {
				// Set the output metadata on the artifact.
				artifact.SetMetaValue("output", "hello")
				return nil
			}),
		}
	}

	func (s *Setter) Read(p []byte) (n int, err error) {
		return s.buffer.Read(p)
	}

	func (s *Setter) Write(p []byte) (n int, err error) {
		return s.buffer.Write(p)
	}

	func (s *Setter) Close() error {
		return s.buffer.Close()
	}

	func main() {
		// Create a new setter.
		setter := NewSetter()

		// Create a new, empty artifact.
		artifact := datura.NewArtifact()

		// Flip the artifact into the setter, and flop it back.
		workflow.NewFlipFlop(artifact, setter)

		// Read the output metadata from the artifact.
		fmt.Println(datura.GetMetaValue[string](artifact, "output"))
		// Output: hello
	}

````
*/
func NewFlipFlop(from io.ReadWriter, to io.ReadWriter) (err error) {
	if _, err = io.Copy(to, from); err != nil {
		return fmt.Errorf("flipflop: copy from->to: %w", err)
	}

	if _, err = io.Copy(from, to); err != nil {
		return fmt.Errorf("flipflop: copy to->from: %w", err)
	}

	return nil
}
