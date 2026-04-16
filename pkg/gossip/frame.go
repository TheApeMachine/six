package gossip

import "io"

/*
frameDelimitedReader wraps an io.Reader whose concrete implementations
(*primitive.Value, *Conn, …) return (n, io.EOF) after each full wire frame
as a delimiter, not as “the stream is finished.”

io.Copy and io.MultiReader treat any io.EOF as end-of-stream, so they stop
after the first frame. This adapter maps a successful frame read that happens
to use io.EOF as the delimiter into (n, nil) so stdlib stream combinators
keep pulling until the underlying reader returns (0, io.EOF).
*/
type frameDelimitedReader struct {
	r io.Reader
}

/*
FrameDelimitedReader returns an io.Reader suitable for io.Copy and
io.LimitReader over framed gossip sources.
*/
func FrameDelimitedReader(r io.Reader) io.Reader {
	return &frameDelimitedReader{r: r}
}

func (reader *frameDelimitedReader) Read(p []byte) (int, error) {
	n, err := reader.r.Read(p)

	if n > 0 && err == io.EOF {
		return n, nil
	}

	return n, err
}
