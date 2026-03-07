package geometry

const ExpectedPrecisionUnity uint16 = 1024

type ExpectedField struct {
	Header    ManifoldHeader
	Support   [4]MacroCube
	Veto      MacroCube
	Precision [5][CubeFaces]uint16
}

func NewExpectedField() ExpectedField {
	field := ExpectedField{}
	field.SetUniformPrecision(ExpectedPrecisionUnity)
	return field
}

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

func ExpectedManifoldFromField(field *ExpectedField) *IcosahedralManifold {
	if field == nil || !field.HasSignal() {
		return nil
	}

	m := field.ToManifold()
	return &m
}

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

func (f *ExpectedField) SetUniformPrecision(weight uint16) {
	for cube := range f.Precision {
		for block := range f.Precision[cube] {
			f.Precision[cube][block] = weight
		}
	}
}

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
