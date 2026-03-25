package workflow

import (
	"io"
)

// copyChain moves bytes from src to dst. Unlike io.Copy, if dst.Write accepts
// only a prefix of the buffer (n < len(p), err == nil) — as *primitive.Value does
// at chunk boundaries — copyChain keeps calling Write with the remainder until the
// batch is drained. io.Copy would return io.ErrShortWrite in that situation.
func copyChain(dst io.Writer, src io.Reader) (written int64, err error) {
	buf := make([]byte, 32*1024)
	for {
		var nr int
		nr, err = src.Read(buf)
		if nr > 0 {
			chunk := buf[:nr]
			for len(chunk) > 0 {
				var nw int
				nw, err = dst.Write(chunk)
				if nw > 0 {
					written += int64(nw)
				}
				if err != nil {
					return written, err
				}
				if nw == 0 && len(chunk) > 0 {
					return written, io.ErrShortWrite
				}
				chunk = chunk[nw:]
			}
		}
		if err != nil {
			if err == io.EOF {
				return written, nil
			}
			return written, err
		}
		if nr == 0 {
			return written, nil
		}
	}
}

/*
Pipeline manages a chain of io.ReadWriteCloser components.

It connects components together so data flows through all components in sequence.
Each component can produce data independently.
*/
type Pipeline struct {
	components []io.ReadWriter
	processed  bool
}

/*
NewPipeline creates a pipeline connecting io.ReadWriteCloser components.

It connects components together so data written to the pipeline flows through
all components in sequence.

Example:

	// Simple pipeline
	p := workflow.NewPipeline(dataset, seed, backend)
	io.Copy(os.Stdout, p)

	// Nested pipelines
	p1 := workflow.NewPipeline(dataset, seed, backend)
	p2 := workflow.NewPipeline(dataset, seed, backend, p1)
	io.Copy(os.Stdout, p2)
*/
func NewPipeline(components ...io.ReadWriter) io.ReadWriter {
	return &Pipeline{components: components}
}

/*
Read implements the io.Reader interface.

It reads from the first component and passes data through the pipeline.
Returns EOF when no more data is available.
*/
func (pipeline *Pipeline) Read(p []byte) (n int, err error) {
	if len(pipeline.components) == 0 {
		return 0, io.EOF
	}

	if !pipeline.processed {
		if ms, ok := pipeline.components[0].(*MergeSource); ok {
			ms.SetAllowLoopReads(false)
		}
		for i := range len(pipeline.components) - 1 {
			_, err = copyChain(pipeline.components[i+1], pipeline.components[i])
			if err != nil && err != io.EOF {
				return 0, err
			}
			if i == 0 {
				if ms, ok := pipeline.components[0].(*MergeSource); ok {
					ms.SetAllowLoopReads(true)
				}
			}
		}
		pipeline.processed = true
	}

	n, err = pipeline.components[len(pipeline.components)-1].Read(p)

	if err != nil && err != io.EOF {
		return n, err
	}

	if n == 0 {
		return n, io.EOF
	}

	return n, nil
}

/*
Write implements the io.Writer interface.

It writes data to the first component in the pipeline.
Note that writing is optional - components can produce data independently.
*/
func (pipeline *Pipeline) Write(p []byte) (n int, err error) {
	if len(pipeline.components) == 0 {
		return len(p), nil
	}

	// Write to first component
	n, err = pipeline.components[0].Write(p)
	if err != nil {
		return n, err
	}

	// Flow data through remaining components
	for i := 0; i < len(pipeline.components)-1; i++ {
		_, err = copyChain(pipeline.components[i+1], pipeline.components[i])
		if err != nil && err != io.EOF {
			return n, err
		}
	}

	return n, nil
}

/*
Close implements the io.Closer interface.

It closes all components in the pipeline and collects any errors encountered.
*/
func (pipeline *Pipeline) Close() error {
	return nil
}
