package gossip

import (
	"io"
	"sort"
	"unsafe"

	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
scoreAlpha is the EMA smoothing coefficient for peer success scores.
Lower values = slower adaptation; higher values = faster forgetting.
*/
const scoreAlpha = 0.1

/*
scorePruneFloor is the minimum score below which a peer is eligible for
pruning during a Reorder call.
*/
const scorePruneFloor = 0.05

/*
ScoredPeer pairs an outbound io.ReadWriteCloser with a floating-point
success score and the peer's affinity fingerprint.

score is updated via an EMA each time a write succeeds or fails downstream.
A successful write (no error) increments toward 1.0; a failed write
decrements toward 0.0. The score drives PriorityRoute ordering so the
most productive path is tried first, creating emergent fast paths without
explicit configuration.
*/
type ScoredPeer struct {
	dst      io.ReadWriteCloser
	affinity []uint64
	score    float64
}

func (sp *ScoredPeer) Dst() io.ReadWriteCloser {
	return sp.dst
}

func (sp *ScoredPeer) Affinity() []uint64 {
	return sp.affinity
}

func (sp *ScoredPeer) Score() float64 {
	return sp.score
}

/*
PriorityRoute is a slice of ScoredPeer that implements io.Writer by writing
to all peers in score-descending order. It is the outbound fan-out mechanism
for both geometry.Field and gossip.Conn.

Routing is not load-balanced — every Write reaches every peer. The priority
ordering matters only for short-circuiting when a caller wraps PriorityRoute
in an io.LimitedWriter or similar combinator that stops after the first
successful write.
*/
type PriorityRoute []ScoredPeer

/*
AddPeer registers an outbound io.ReadWriteCloser peer. The peer's affinity is
used by AffinityFilter wrappers to decide whether a given Value frame should
be forwarded to it.
*/
func (route *PriorityRoute) AddPeer(peer io.ReadWriteCloser, affinity []uint64) {
	*route = append(*route, ScoredPeer{
		dst:      peer,
		affinity: affinity,
	})
}

/*
Write serialises p to every peer in priority order. Each peer's score is
updated after the write: success nudges the score up toward 1.0, error
nudges it down toward 0.0. Returns the byte count of the last successful
write, or the first error encountered.
*/
func (route PriorityRoute) Write(p []byte) (int, error) {
	var (
		written int
		lastErr error
	)

	for idx := range route {
		n, err := route[idx].dst.Write(p)

		if err == nil {
			route[idx].score += scoreAlpha * (1.0 - route[idx].score)
			written = n
		} else {
			route[idx].score += scoreAlpha * (0.0 - route[idx].score)
			lastErr = err
		}
	}

	if written > 0 {
		return written, nil
	}

	return 0, lastErr
}

/*
Read is not supported on PriorityRoute — it is an outbound-only abstraction.
*/
func (route PriorityRoute) Read(_ []byte) (int, error) {
	return 0, io.ErrClosedPipe
}

/*
Close closes all peers in the route.
*/
func (route PriorityRoute) Close() error {
	for _, peer := range route {
		if peer.dst != nil {
			peer.dst.Close()
		}
	}

	return nil
}

/*
Reorder sorts the route descending by score, promoting high-success peers to
the front. Peers with score below scorePruneFloor are removed. Call this from
the field or conn Cycle after updating scores via Write.
*/
func (route *PriorityRoute) Reorder() {
	filtered := (*route)[:0]

	for _, peer := range *route {
		if peer.score >= scorePruneFloor || peer.score == 0 {
			filtered = append(filtered, peer)
		}
	}

	*route = filtered

	sort.Slice(*route, func(i, j int) bool {
		return (*route)[i].score > (*route)[j].score
	})
}

/*
AffinityFilter wraps an io.Writer and only forwards frames whose Value
affinity is within hammingBudget bits of target. Frames outside the budget
are silently dropped — the write returns len(p), nil so the caller does not
treat the drop as an error.

Affinity words live at a fixed offset (words 123–127, bytes 984–1024) in the
1024-byte Value wire frame.
*/
type AffinityFilter struct {
	dst           io.Writer
	target        []uint64
	hammingBudget int
}

/*
NewAffinityFilter wraps dst so only frames addressed to target (within
hammingBudget bits Hamming distance) are forwarded.
*/
func NewAffinityFilter(dst io.Writer, target []uint64, hammingBudget int) *AffinityFilter {
	return &AffinityFilter{
		dst:           dst,
		target:        target,
		hammingBudget: hammingBudget,
	}
}

/*
Write forwards p to the wrapped destination only when the affinity words
embedded in p (at Value wire offset 123–127) are within hammingBudget bits
of target. Drops the frame cleanly otherwise.
*/
func (filter *AffinityFilter) Write(p []byte) (int, error) {
	const (
		affinityWordStart = 123
		affinityWordCount = 5
		wordSize          = 8
		affinityByteStart = affinityWordStart * wordSize
		affinityByteEnd   = affinityByteStart + affinityWordCount*wordSize
	)

	if len(p) < affinityByteEnd {
		return 0, io.ErrShortBuffer
	}

	var frameAffinity [affinityWordCount]uint64

	src := (*primitive.Value)(unsafe.Pointer(&p[0]))

	for wordIdx := range frameAffinity {
		frameAffinity[wordIdx] = (*src)[affinityWordStart+wordIdx]
	}

	distance := geometry.AffinityHammingDistance(frameAffinity[:], filter.target[:])
	if distance > filter.hammingBudget {
		return len(p), nil
	}

	return filter.dst.Write(p)
}
