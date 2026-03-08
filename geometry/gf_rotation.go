package geometry

/*
GFRotation represents a composable affine transformation over GF(257):

	f(x) = (A·x + B) mod 257

where A ∈ {1..256} (non-zero, so the map is a bijection) and B ∈ {0..256}.

Composition is closed: if rot₁ = (a₁, b₁) and rot₂ = (a₂, b₂), then

	rot₂ ∘ rot₁ = ( a₂·a₁ mod 257,  a₂·b₁ + b₂ mod 257 )

This eliminates physical data movement entirely. Instead of shuffling
257 chords (16KB) per rotation, we compose two integers in O(1).
*/
type GFRotation struct {
	A uint16 // multiplicative coefficient, must be non-zero (1 for identity)
	B uint16 // additive offset
}

/*
RotationForByte maps a single byte to an injective GF(257) transform.
*/
func RotationForByte(b byte) GFRotation {
	val := uint16(b)
	return GFRotation{
		A: val + 1,
		B: (val * 31) % 257,
	}
}

/*
IdentityRotation returns the identity transformation f(x) = x.
A=1, B=0; every face maps to itself. Used as the neutral element for composition.
*/
func IdentityRotation() GFRotation {
	return GFRotation{A: 1, B: 0}
}

/*
Forward maps a logical face index to its physical position under this rotation.
Computes (A·face + B) mod 257 with proper handling of negative modulo.
*/
func (r GFRotation) Forward(face int) int {
	raw := int(r.A)*face + int(r.B)
	return (raw%CubeFaces + CubeFaces) % CubeFaces
}

/*
Reverse maps a physical face index back to its logical byte value.
Computes the inverse affine transform: finds A⁻¹ in GF(257) then (face - B)·A⁻¹ mod 257.
*/
func (r GFRotation) Reverse(face int) int {
	var invA int
	for i := 1; i < CubeFaces; i++ {
		if (int(r.A)*i)%CubeFaces == 1 {
			invA = i
			break
		}
	}

	val := (face - int(r.B)) % CubeFaces
	if val < 0 {
		val += CubeFaces
	}

	return (invA * val) % CubeFaces
}

/*
Compose returns the rotation equivalent to applying r first, then other.
other(r(x)) = other.A·(r.A·x + r.B) + other.B = (other.A·r.A)·x + (other.A·r.B + other.B).
Enables O(1) composition without data movement.
*/
func (r GFRotation) Compose(other GFRotation) GFRotation {
	return GFRotation{
		A: uint16((int(other.A) * int(r.A)) % CubeFaces),
		B: uint16((int(other.A)*int(r.B) + int(other.B)) % CubeFaces),
	}
}

/*
The three micro-rotations as GF(257) affine transforms.
Match the permutation tables in permutation.go; used for O(1) composition instead of table lookup.
*/
var (
	// RotationX is a translation: f(x) = (x + 1) mod 257
	RotationX = GFRotation{A: 1, B: 1}

	// RotationY is a dilation by primitive root 3: f(x) = (3·x) mod 257
	RotationY = GFRotation{A: 3, B: 0}

	// RotationZ is an affine combination: f(x) = (3·x + 1) mod 257
	RotationZ = GFRotation{A: 3, B: 1}
)

/*
RotationEvent maps a canonical GF(257) micro-rotation back to the topological event.
Returns (event, true) if rot matches RotationX/Y/Z or XX; (0, false) otherwise.
*/
func RotationEvent(rot GFRotation) (int, bool) {
	switch rot {
	case RotationX:
		return EventDensitySpike, true
	case RotationY:
		return EventPhaseInversion, true
	case RotationZ:
		return EventDensityTrough, true
	case RotationX.Compose(RotationX):
		return EventLowVarianceFlux, true
	default:
		return 0, false
	}
}

/*
EventRotation returns the GFRotation corresponding to a topological event.
EventDensitySpike→X, EventPhaseInversion→Y, EventDensityTrough→Z, EventLowVarianceFlux→XX.
Unknown events return IdentityRotation.
*/
func EventRotation(event int) GFRotation {
	switch event {
	case EventDensitySpike:
		return RotationX
	case EventPhaseInversion:
		return RotationY
	case EventDensityTrough:
		return RotationZ
	case EventLowVarianceFlux:
		// Double RotationX: f(x) = ((x+1)+1) mod 257 = (x+2) mod 257
		return RotationX.Compose(RotationX)
	default:
		return IdentityRotation()
	}
}

/*
ComposeEvents composes all event rotations in sequence.
Returns a single GFRotation equivalent to applying each EventRotation in order.
*/
func ComposeEvents(events []int) GFRotation {
	rot := IdentityRotation()
	for _, ev := range events {
		rot = rot.Compose(EventRotation(ev))
	}
	return rot
}
