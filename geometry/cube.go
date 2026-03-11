package geometry

import "github.com/theapemachine/six/data"

const cubeInjectionLanes = 2

/*
Side is one of the 6 faces of the 3D cube.
Each side has 4 rotational orientations (0°, 90°, 180°, 270°).
Each orientation holds 257 chords — 256 data faces + 1 control face (index 256).
*/
type Side [4][CubeFaces]data.Chord

/*
Cube is the fundamental storage and compute primitive.
6 sides × 4 rotations × 257 chords = 6,168 chord positions.
Each position holds a 257-bit chord.

The chord at index 256 of each side×rotation is the control chord —
never written by data ingestion, used to determine edge opcodes
between connected cubes in the graph.
*/
type Cube struct {
	Sides [6]Side
}

/*
NewCube allocates an empty cube with all chords zeroed.
*/
func NewCube() Cube {
	return Cube{}
}

/*
Get returns the chord at the specified side, rotation, and face index.
*/
func (c *Cube) Get(side, rot, face int) data.Chord {
	return c.Sides[side][rot][face]
}

/*
Set writes a chord at the specified side, rotation, and face index.
*/
func (c *Cube) Set(side, rot, face int, chord data.Chord) {
	c.Sides[side][rot][face] = chord
}

/*
ORInto preserves the legacy broadcast-style write path used outside the
update-7 cortex port.
*/
func (c *Cube) ORInto(side, rot, face int, chord *data.Chord) {
	dst := &c.Sides[side][rot][face]
	dst[0] |= chord[0]
	dst[1] |= chord[1]
	dst[2] |= chord[2]
	dst[3] |= chord[3]
	dst[4] |= chord[4]
	dst[5] |= chord[5]
	dst[6] |= chord[6]
	dst[7] |= chord[7]
}

/*
Face256 returns the control chord for a given side and rotation.
This chord determines edge opcodes when two cubes are connected.
*/
func (c *Cube) Face256(side, rot int) data.Chord {
	return c.Sides[side][rot][256]
}

/*
SetFace256 writes directly to the control chord for a given side and rotation.
*/
func (c *Cube) SetFace256(side, rot int, chord data.Chord) {
	c.Sides[side][rot][256] = chord
}

/*
Wipe zeroes out every chord in the cube.
*/
func (c *Cube) Wipe() {
	*c = Cube{}
}

/*
WipeSide zeroes out all chords on a specific side and rotation.
*/
func (c *Cube) WipeSide(side, rot int) {
	c.Sides[side][rot] = [CubeFaces]data.Chord{}
}

func cubePatch(logicalFace int, seed data.Chord, lane int) (side, rot int) {
	aperture := (data.ChordBin(&seed) + logicalFace + lane*53) % (6 * 4)

	return aperture % 6, (aperture / 6) % 4
}

/*
Inject writes payload into a small deterministic subset of side-rotation lanes
instead of broadcasting to all 24 patches. This turns the side and rotation
axes into real state carriers rather than duplicated storage.
*/
func (c *Cube) Inject(logicalFace int, payload, carrier data.Chord, rotState GFRotation) {
	if logicalFace < 0 || logicalFace >= CubeFaces || payload.ActiveCount() == 0 {
		return
	}

	physicalFace := rotState.Forward(logicalFace)
	seed := payload

	if carrier.ActiveCount() > 0 {
		seed = data.ChordOR(&seed, &carrier)
	} else {
		stateChord := rotState.StateChord()
		seed = data.ChordOR(&seed, &stateChord)
	}

	for lane := 0; lane < cubeInjectionLanes; lane++ {
		side, rot := cubePatch(logicalFace, seed, lane)
		existing := c.Get(side, rot, physicalFace)
		c.Set(side, rot, physicalFace, data.ChordOR(&existing, &payload))
	}
}

/*
InjectControl writes a control-plane chord into a deterministic subset of edge
ports on face 256.
*/
func (c *Cube) InjectControl(program, carrier data.Chord, rotState GFRotation) {
	if program.ActiveCount() == 0 {
		return
	}

	seed := program

	if carrier.ActiveCount() > 0 {
		seed = data.ChordOR(&seed, &carrier)
	} else {
		stateChord := rotState.StateChord()
		seed = data.ChordOR(&seed, &stateChord)
	}

	for lane := 0; lane < cubeInjectionLanes; lane++ {
		side, rot := cubePatch(256, seed, lane)
		gate := c.Face256(side, rot)
		c.SetFace256(side, rot, data.ChordOR(&gate, &program))
	}
}
