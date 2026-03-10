package cortex

import (
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
)

/*
Token is a Chord in transit between cortex nodes.

Tokens carry no symbolic type label ("DATA", "SIGNAL", "LAW").
Their identity is their bit pattern. Special behaviors are triggered by
structural resonance properties of the chord:

  - A rotation token is detected by its geometric signature (exactly 2 active faces
    encoding (A, B) coefficients for a GF(257) affine transform).
  - All other tokens are treated as accumulation data.

TTL prevents infinite loops: a token dies after TTL hops regardless of
whether any node consumes it.
*/
type Token struct {
	Chord       data.Chord
	LogicalFace int // pure 0-255 byte identity, or 256
	Origin      int // source node ID (-1 for external injection / bedrock pull)
	TTL         int // remaining hop budget

	Op    Opcode              // computed geometric opcode
	Carry geometry.GFRotation // the GF(257) lens of the sender

	// NEW: Computation vs Physics bifurcation
	IsSignal   bool       // true = pure computational routing, bypasses physical state update
	SignalMask data.Chord // bit-channel constraints for routing this signal
}

var controlPlaneMask = func() data.Chord {
	var mask data.Chord
	mask.Set(256)

	return mask
}()

/*
NewRotationToken creates a token carrying a GF(257) geometric action.
It uses OpCompose to directly apply the transform to the receiver's lens.
*/
func NewRotationToken(rot geometry.GFRotation, origin int) Token {
	return Token{
		LogicalFace: 256,
		Origin:      origin,
		TTL:         defaultTTL,
		Op:          OpCompose,
		Carry:       rot,
		IsSignal:    false, // By default physics
	}
}

/*
NewDataToken wraps an existing chord as a data-carrying token.
*/
func NewDataToken(chord data.Chord, logicalFace int, origin int) Token {
	return Token{
		Chord:       chord,
		LogicalFace: logicalFace,
		Origin:      origin,
		TTL:         defaultTTL,
		IsSignal:    false, // By default physics
	}
}

/*
NewSignalToken creates a token that threads the needle for pure computation,
bypassing the global equilibrium attractor to travel over open face-channels.
*/
func NewSignalToken(chord data.Chord, mask data.Chord, origin int) Token {
	return Token{
		Chord:       chord,
		LogicalFace: 256,
		Origin:      origin,
		TTL:         defaultTTL * 2, // Signals typically traverse further
		IsSignal:    true,
		SignalMask:  mask,
	}
}

func chordControlPlane(chord data.Chord) data.Chord {
	return data.ChordAND(&chord, &controlPlaneMask)
}


