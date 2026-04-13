package vm

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"

	"github.com/smallnest/ringbuffer"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/programmer"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/transport"
	"github.com/theapemachine/six/pkg/viz"
)

var bufPool = sync.Pool{
	New: func() any {
		return make([]byte, core.Cfg.Value.Region.MaxTokenIngestBytes())
	},
}

/*
wireFrameBufPool holds buffers sized for Value.Bytes egress when a Pipeline
tees wire frames while Publish runs on live *Value instances.
*/
var wireFrameBufPool = sync.Pool{
	New: func() any {
		return make([]byte, core.Cfg.Value.Bytes)
	},
}

type Tokenizer struct {
	ctx    context.Context
	cancel context.CancelFunc
	err    error
	rb     *ringbuffer.RingBuffer
	pr     *ringbuffer.PipeReader
	pw     *ringbuffer.PipeWriter
	// pipeWriterClosed gates ClosePipeWriter so Load can unblock the ingest
	// side when the drain path fails while the ring is full.
	pipeWriterClosed uint32
	queue            *pool.Queue
	current          *primitive.Value
	mintGoldClass    int
	mintGoldOK       bool
}

type tokenizerOption func(*Tokenizer)

func NewTokenizer(
	ctx context.Context,
	queue *pool.Queue,
	opts ...tokenizerOption,
) (*Tokenizer, error) {
	if err := validate.Require(map[string]any{
		"queue": queue,
	}); err != nil {
		return nil, errnie.Error(err)
	}

	ctx, cancel := context.WithCancel(ctx)

	rb := ringbuffer.New(
		int(
			uint64(core.Cfg.Value.Region.MaxTokenIngestBytes()) * uint64(core.Cfg.Value.Bytes),
		),
	)

	pr, pw := rb.Pipe()

	tokenizer := &Tokenizer{
		ctx:    ctx,
		cancel: cancel,
		rb:     rb,
		pr:     pr,
		pw:     pw,
		queue:  queue,
	}

	for _, opt := range opts {
		opt(tokenizer)
	}

	return tokenizer, validate.Require(map[string]any{
		"tokenizer": tokenizer,
		"ctx":       tokenizer.ctx,
		"cancel":    tokenizer.cancel,
		"rb":        tokenizer.rb,
		"queue":     tokenizer.queue,
	})
}

func (tokenizer *Tokenizer) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	buf := bufPool.Get().([]byte)
	buf = buf[:cap(buf)]
	defer bufPool.Put(buf[:0])

	rbN, rbErr := tokenizer.rb.Read(buf)

	if rbN > 0 {
		if _, adoptErr := tokenizer.adoptChunk(buf[:rbN]); adoptErr != nil {
			return 0, adoptErr
		}

		frameN, frameErr := tokenizer.current.Read(p)

		// Value.Read returns io.EOF after each full frame (single-shot Reader).
		// That is not tokenizer end-of-stream while the ring still has bytes.
		if frameN > 0 && errors.Is(frameErr, io.EOF) {
			frameErr = nil
		}

		if rbErr != nil && frameN > 0 && errors.Is(rbErr, io.EOF) {
			rbErr = nil
		}

		if rbErr != nil {
			return frameN, errnie.Error(rbErr)
		}

		return frameN, frameErr
	}

	if rbErr != nil {
		return 0, errnie.Error(rbErr)
	}

	return 0, nil
}

/*
adoptChunk mints one or more *primitive.Value segments from a raw ingest chunk
(via primitive.NewValue), chains Prev/Next across segments, then links the
first new segment to the previous tokenizer.current (if any). The previous
current is closed after writing its Next to the new head.

Chunk length is at most MaxTokenIngestBytes so a single chunk usually yields
one segment; longer logical payloads still never truncate because NewValue
splits across segments inside one call.

Returns every new segment (ordered); tokenizer.current becomes the tail.
*/
func (tokenizer *Tokenizer) adoptChunk(chunk []byte) ([]*primitive.Value, error) {
	segments, err := primitive.NewValue(chunk)

	if err != nil {
		tokenizer.err = err

		return nil, errnie.Error(err)
	}

	if tokenizer.mintGoldOK {
		word := kernel.GoldLabelWord(tokenizer.mintGoldClass)

		for _, seg := range segments {
			seg.Set(kernel.PropertiesStartWord, word)
		}
	}

	old := tokenizer.current
	first := segments[0]
	last := segments[len(segments)-1]

	if old != nil {
		first.Set(
			core.Cfg.Value.Region.Prev.Start,
			old.ID(),
		)

		old.Set(
			core.Cfg.Value.Region.Next.Start,
			first.ID(),
		)

		_ = old.Close()
	}

	tokenizer.current = last

	return segments, nil
}

/*
AdoptLabeledSample runs adoptChunk for one dataset row and, when goldOK is true,
stamps kernel.PropertiesStartWord with kernel.GoldLabelWord(goldClass) on every
minted segment before Prev/Next chaining.

This is the ingest path for discrete supervision: the ring-buffer Copy path cannot
attach a per-sample class without chunk straddling, so Machine.Load calls this
synchronously when the provider implements data.LabeledIngestFeed.
*/
func (tokenizer *Tokenizer) AdoptLabeledSample(payload []byte, goldClass int, goldOK bool) ([]*primitive.Value, error) {
	tokenizer.mintGoldClass = goldClass
	tokenizer.mintGoldOK = goldOK

	defer func() {
		tokenizer.mintGoldOK = false
		tokenizer.mintGoldClass = 0
	}()

	return tokenizer.adoptChunk(payload)
}

/*
IngestSample mints Values from sample.Text with primitive.NewValue (Morton-coded
token slabs). When Label is non-empty, every minted segment receives the same
Properties word from kernel.LabelPropertiesWord. Segments chain into the
tokenizer’s Prev/Next list; each is affinity-installed and published like
DrainPublishedValues, without using the ring buffer.
*/
func (tokenizer *Tokenizer) IngestSample(
	ctx context.Context,
	sample data.Sample,
	publishers []transport.Publishable,
) error {
	if tokenizer == nil {
		return errnie.Error(errors.New("vm.Tokenizer.IngestSample: nil Tokenizer"))
	}

	if len(publishers) == 0 {
		return errnie.Error(errors.New("vm.Tokenizer.IngestSample: need at least one Publishable"))
	}

	if ctx != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
	}

	if len(sample.Text) == 0 {
		return nil
	}

	segments, err := primitive.NewValue(sample.Text)

	if err != nil {
		tokenizer.err = err

		return errnie.Error(err)
	}

	if len(sample.Label) > 0 {
		word := kernel.LabelPropertiesWord(sample.Label)

		for _, seg := range segments {
			seg.Set(kernel.PropertiesStartWord, word)
		}
	}

	old := tokenizer.current
	first := segments[0]
	last := segments[len(segments)-1]

	if old != nil {
		first.Set(
			core.Cfg.Value.Region.Prev.Start,
			old.ID(),
		)

		old.Set(
			core.Cfg.Value.Region.Next.Start,
			first.ID(),
		)

		_ = old.Close()
	}

	tokenizer.current = last

	traceLabel := ""

	for _, seg := range segments {
		viz.DefaultBus.Publish(viz.TokenizerEmitEvent(seg.ID(), traceLabel, seg.String()))

		installer := programmer.Installer{}
		if installErr := installer.InstallProgram(seg, "affinity"); installErr != nil {
			return errnie.Error(installErr)
		}

		for _, publisher := range publishers {
			var pubErr error

			if labeling, ok := publisher.(transport.LabelingPublishable); ok {
				_, pubErr = labeling.PublishLabeled(traceLabel, seg)
			} else {
				_, pubErr = publisher.Publish(seg)
			}

			if pubErr != nil {
				return pubErr
			}
		}
	}

	return nil
}

/*
DrainPublishedValues drains raw ingest from the ring into NewValue-sized
chunks, publishes each live *Value to every sink, and optionally writes one
wire frame per value to frameTee for Pipeline egress.

It mirrors Read's chunking and prev/next linking without ValueFromWireFrame.
*/
func (tokenizer *Tokenizer) DrainPublishedValues(
	ctx context.Context,
	label string,
	publishers []transport.Publishable,
	frameTee io.Writer,
) (err error) {
	if tokenizer == nil {
		return errnie.Error(errors.New("vm.Tokenizer.DrainPublishedValues: nil Tokenizer"))
	}

	buf := bufPool.Get().([]byte)
	buf = buf[:cap(buf)]
	defer bufPool.Put(buf[:0])

	frameNeed := core.Cfg.Value.Bytes

	var frameBacking []byte

	if frameTee != nil {
		raw := wireFrameBufPool.Get().([]byte)
		raw = raw[:cap(raw)]

		if cap(raw) < frameNeed {
			raw = make([]byte, frameNeed)
		}

		frameBacking = raw

		defer func() {
			wireFrameBufPool.Put(raw)
		}()
	}

	for {
		if ctx != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
		}

		rbN, rbErr := tokenizer.rb.Read(buf)

		if rbN > 0 {
			minted, adoptErr := tokenizer.adoptChunk(buf[:rbN])

			if adoptErr != nil {
				return adoptErr
			}

			for _, seg := range minted {
				viz.DefaultBus.Publish(viz.TokenizerEmitEvent(seg.ID(), label, seg.String()))

				installer := programmer.Installer{}
				if installErr := installer.InstallProgram(seg, "affinity"); installErr != nil {
					return errnie.Error(installErr)
				}

				for _, publisher := range publishers {
					var pubErr error

					if labeling, ok := publisher.(transport.LabelingPublishable); ok {
						_, pubErr = labeling.PublishLabeled(label, seg)
					} else {
						_, pubErr = publisher.Publish(seg)
					}

					if pubErr != nil {
						return pubErr
					}
				}

				if frameTee != nil {
					frameBuf := frameBacking[:frameNeed]
					frameN, readErr := seg.Read(frameBuf)

					if frameN > 0 && errors.Is(readErr, io.EOF) {
						readErr = nil
					}

					if readErr != nil {
						return errnie.Error(readErr)
					}

					if _, wErr := frameTee.Write(frameBuf[:frameN]); wErr != nil {
						return wErr
					}
				}
			}

			if rbErr != nil {
				if errors.Is(rbErr, io.EOF) {
					return nil
				}

				return errnie.Error(rbErr)
			}

			continue
		}

		if rbErr != nil {
			if errors.Is(rbErr, io.EOF) {
				return nil
			}

			return errnie.Error(rbErr)
		}

		return nil
	}
}

/*
IngestReader copies r through the tokenizer write side, closes the pipe
writer once, drains minted *primitive.Value records to publishers with
label, then ResetAfterEOF for another bounded reader.

One label applies to every Publish for that reader, so a logical sample
split across multiple fixed-width Values needs no delimiter bytes—only
that those bytes are copied before the next IngestReader call with the
next label.

Do not use IngestReader concurrently with transport.Pipeline on the same
Tokenizer; Pipeline already runs a background DrainPublishedValues on this
ring.
*/
func (tokenizer *Tokenizer) IngestReader(
	ctx context.Context,
	r io.Reader,
	label string,
	publishers []transport.Publishable,
	frameTee io.Writer,
) (err error) {
	if tokenizer == nil {
		return errnie.Error(errors.New("vm.Tokenizer.IngestReader: nil Tokenizer"))
	}

	if len(publishers) == 0 {
		return errnie.Error(errors.New("vm.Tokenizer.IngestReader: need at least one Publishable"))
	}

	if err := validate.Require(map[string]any{
		"pw": tokenizer.pw,
		"rb": tokenizer.rb,
	}); err != nil {
		return errnie.Error(err)
	}

	errChan := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(tokenizer, r)
		if closeErr := tokenizer.ClosePipeWriter(); closeErr != nil && copyErr == nil {
			copyErr = closeErr
		}
		errChan <- copyErr
	}()

	drainErr := tokenizer.DrainPublishedValues(ctx, label, publishers, frameTee)
	copyErr := <-errChan

	if drainErr != nil {
		if copyErr != nil {
			return errnie.Error(errors.Join(drainErr, copyErr))
		}

		return errnie.Error(drainErr)
	}

	if copyErr != nil {
		return errnie.Error(copyErr)
	}

	tokenizer.ResetAfterEOF()

	return nil
}

func (tokenizer *Tokenizer) Write(p []byte) (n int, err error) {
	n, err = tokenizer.pw.Write(p)
	if n > 0 {
		viz.DefaultBus.Publish(viz.TokenizerChunkEvent(n))
	}
	return
}

/*
CloseWrite satisfies transport.FramedBytePipe: end the raw ingest side after
io.Copy from the dataset finishes.
*/
func (tokenizer *Tokenizer) CloseWrite() error {
	return tokenizer.ClosePipeWriter()
}

var _ transport.FramedBytePipe = (*Tokenizer)(nil)

/*
ClosePipeWriter signals end-of-stream to readers so blocked Read calls
unblock after the ring drains. Pair with ResetAfterEOF before reuse.
*/
func (tokenizer *Tokenizer) ClosePipeWriter() error {
	if tokenizer == nil || tokenizer.pw == nil {
		return nil
	}

	if !atomic.CompareAndSwapUint32(&tokenizer.pipeWriterClosed, 0, 1) {
		return nil
	}

	return tokenizer.pw.Close()
}

/*
ResetAfterEOF clears tokenizer-side state and resets the ring after the
write end was closed and reads have drained. The same PipeReader and
PipeWriter handles remain valid for the next ingest.
*/
func (tokenizer *Tokenizer) ResetAfterEOF() {
	if tokenizer == nil || tokenizer.rb == nil {
		return
	}

	if tokenizer.current != nil {
		_ = tokenizer.current.Close()
		tokenizer.current = nil
	}

	atomic.StoreUint32(&tokenizer.pipeWriterClosed, 0)
	tokenizer.rb.Reset()
}

func (tokenizer *Tokenizer) Close() (err error) {
	tokenizer.cancel()

	var curClose error

	if tokenizer.current != nil {
		curClose = tokenizer.current.Close()
		tokenizer.current = nil
	}

	return errors.Join(
		err,
		curClose,
		tokenizer.pw.Close(),
		tokenizer.pr.Close(),
	)
}

var _ transport.PublishedValueDrainer = (*Tokenizer)(nil)
