package geometry

import "github.com/theapemachine/six/data"

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
