package geometry

import (
	"reflect"
	"testing"
)

func TestNewExpectedField_InitializesUnityPrecision(t *testing.T) {
	field := NewExpectedField()

	for cube := range field.Precision {
		for block := range field.Precision[cube] {
			if field.Precision[cube][block] != ExpectedPrecisionUnity {
				t.Fatalf("precision[%d][%d]=%d, want %d", cube, block, field.Precision[cube][block], ExpectedPrecisionUnity)
			}
		}
	}
}

func TestExpectedField_ManifoldRoundTrip(t *testing.T) {
	var manifold IcosahedralManifold
	manifold.Header.SetState(1)
	manifold.Header.SetRotState(17)
	manifold.Header.IncrementWinding()
	manifold.Header.IncrementWinding()

	manifold.Cubes[0][0].Set(3)
	manifold.Cubes[1][5].Set(31)
	manifold.Cubes[2][11].Set(67)
	manifold.Cubes[3][26].Set(127)
	manifold.Cubes[4][8].Set(255)

	field := ExpectedFieldFromManifold(&manifold)
	restored := field.ToManifold()

	if !reflect.DeepEqual(restored, manifold) {
		t.Fatalf("round-trip mismatch")
	}
}

func TestExpectedFieldFromManifold_NilInput(t *testing.T) {
	got := ExpectedFieldFromManifold(nil)
	want := NewExpectedField()

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected field from nil manifold")
	}
}

func TestExpectedManifoldFromField_NilInput(t *testing.T) {
	if got := ExpectedManifoldFromField(nil); got != nil {
		t.Fatalf("expected nil manifold pointer, got non-nil")
	}
}

func TestExpectedManifoldFromField_EmptyField(t *testing.T) {
	empty := NewExpectedField()
	if empty.HasSignal() {
		t.Fatalf("empty expected field unexpectedly reports signal")
	}

	if got := ExpectedManifoldFromField(&empty); got != nil {
		t.Fatalf("expected nil manifold for empty expected field")
	}
}

func TestExpectedManifoldFromField_NonEmptyField(t *testing.T) {
	field := NewExpectedField()
	field.Support[0][0].Set(11)

	if !field.HasSignal() {
		t.Fatalf("non-empty expected field did not report signal")
	}

	got := ExpectedManifoldFromField(&field)
	if got == nil {
		t.Fatalf("expected non-nil manifold for non-empty expected field")
	}

	if !got.Cubes[0][0].Has(11) {
		t.Fatalf("expected mapped manifold to carry support bit")
	}
}
