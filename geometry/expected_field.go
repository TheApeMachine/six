package geometry

/*
ExpectedPrecisionUnity is the default per-block precision weight (1024).
Used when initializing a fresh ExpectedField via SetUniformPrecision.
*/
const ExpectedPrecisionUnity uint16 = 1024

/*
ExpectedField holds the expected manifold state plus per-block precision weights.
Support[0..3] and Veto (cube 4) mirror IcosahedralManifold cubes.
Precision[cube][block] weights each block for weighted aggregation.
*/
type ExpectedField struct {
	Header    ManifoldHeader
	Support   [4]MacroCube
	Veto      MacroCube
	Precision [5][CubeFaces]uint16
}

/*
NewExpectedField allocates a fresh field with uniform precision ExpectedPrecisionUnity.
*/
func NewExpectedField() ExpectedField {
	field := ExpectedField{}
	field.SetUniformPrecision(ExpectedPrecisionUnity)
	return field
}

/*
ExpectedFieldFromManifold copies manifold state into an ExpectedField.
Support[0..3] ← Cubes[0..3], Veto ← Cubes[4]. Nil manifold returns default field.
*/
func ExpectedFieldFromManifold(m *IcosahedralManifold) ExpectedField {
	field := NewExpectedField()
	if m == nil {
		return field
	}

	field.Header = m.Header
	for cube := 0; cube < 4; cube++ {
		field.Support[cube] = m.Cubes[cube]
	}
	field.Veto = m.Cubes[4]

	return field
}

/*
ExpectedManifoldFromField reconstructs an IcosahedralManifold from an ExpectedField.
Returns nil if field is nil or HasSignal is false.
*/
func ExpectedManifoldFromField(field *ExpectedField) *IcosahedralManifold {
	if field == nil || !field.HasSignal() {
		return nil
	}

	m := field.ToManifold()
	return &m
}

/*
HasSignal returns true if any Support or Veto block has active bits.
Used to decide whether ExpectedManifoldFromField can produce a non-empty manifold.
*/
func (f *ExpectedField) HasSignal() bool {
	if f == nil {
		return false
	}

	for cube := range f.Support {
		for block := range f.Support[cube] {
			if f.Support[cube][block].ActiveCount() > 0 {
				return true
			}
		}
	}

	for block := range f.Veto {
		if f.Veto[block].ActiveCount() > 0 {
			return true
		}
	}

	return false
}

/*
SetUniformPrecision sets all Precision[cube][block] to weight.
Typically called with ExpectedPrecisionUnity during initialization.
*/
func (f *ExpectedField) SetUniformPrecision(weight uint16) {
	for cube := range f.Precision {
		for block := range f.Precision[cube] {
			f.Precision[cube][block] = weight
		}
	}
}

/*
ToManifold copies Support and Veto into an IcosahedralManifold.
Header is preserved; Cubes[0..3] ← Support, Cubes[4] ← Veto.
*/
func (f *ExpectedField) ToManifold() IcosahedralManifold {
	if f == nil {
		return IcosahedralManifold{}
	}

	m := IcosahedralManifold{Header: f.Header}
	for cube := 0; cube < 4; cube++ {
		m.Cubes[cube] = f.Support[cube]
	}
	m.Cubes[4] = f.Veto

	return m
}
