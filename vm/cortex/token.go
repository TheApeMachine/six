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
	Chord  data.Chord
	Origin int // source node ID (-1 for external injection / bedrock pull)
	TTL    int // remaining hop budget
}

/*
IsRotational detects whether this token encodes a GF(257) rotation transform.

Detection heuristic: a rotation chord has exactly 2 active bits in the
257-bit logical range. The two active bit positions encode (A, B) coefficients
for the affine transform f(x) = (A·x + B) mod 257.

This is a structural signature — a normal data chord produced by BaseChord
or ChordOR will have 5+ active bits. A rotation chord is deliberately sparse.
*/
func (t *Token) IsRotational() bool {
	return t.Chord.ActiveCount() == 2
}

/*
DecodeRotation extracts (A, B) coefficients from a rotation chord.

NewRotationToken sets bits at positions A and B. ChordPrimeIndices returns
indices in ascending order. By convention, the LARGER index is always the
multiplicative coefficient A (which must be ≥1 for a valid GF(257) bijection),
and the smaller index is the additive offset B.

Precondition: IsRotational() must be true.

Returns IdentityRotation if the chord is degenerate.
*/
func (t *Token) DecodeRotation() geometry.GFRotation {
	indices := data.ChordPrimeIndices(&t.Chord)
	if len(indices) < 2 {
		return geometry.IdentityRotation()
	}
	// indices are ascending: [smaller, larger].
	// A = larger (multiplicative, must be ≥1), B = smaller (additive).
	a, b := uint16(indices[1]), uint16(indices[0])
	if a == 0 {
		return geometry.IdentityRotation()
	}
	return geometry.GFRotation{A: a, B: b}
}

/*
NewRotationToken creates a LAW-equivalent token carrying a GF(257) rotation.
The chord is deliberately sparse: exactly 2 bits set at positions A and B.
*/
func NewRotationToken(rot geometry.GFRotation, origin int) Token {
	var c data.Chord
	c.Set(int(rot.A))
	c.Set(int(rot.B))
	return Token{
		Chord:  c,
		Origin: origin,
		TTL:    defaultTTL,
	}
}

/*
NewDataToken wraps an existing chord as a data-carrying token.
*/
func NewDataToken(chord data.Chord, origin int) Token {
	return Token{
		Chord:  chord,
		Origin: origin,
		TTL:    defaultTTL,
	}
}
