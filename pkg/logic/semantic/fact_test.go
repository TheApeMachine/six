package semantic

import (
	"testing"

	"github.com/theapemachine/six/pkg/numeric"
)

func TestFactRoundTrip(t *testing.T) {
	fact := Fact{
		Subject:  "subj",
		Link:     "link",
		Object:   "obj",
		Phase:    numeric.Phase(42),
		Temporal: numeric.Phase(7),
		Negated:  true,
	}

	if fact.Subject != "subj" {
		t.Fatalf("Subject: want subj, got %q", fact.Subject)
	}
	if fact.Link != "link" {
		t.Fatalf("Link: want link, got %q", fact.Link)
	}
	if fact.Object != "obj" {
		t.Fatalf("Object: want obj, got %q", fact.Object)
	}
	if fact.Phase != numeric.Phase(42) {
		t.Fatalf("Phase: want 42, got %v", fact.Phase)
	}
	if fact.Temporal != numeric.Phase(7) {
		t.Fatalf("Temporal: want 7, got %v", fact.Temporal)
	}
	if !fact.Negated {
		t.Fatalf("Negated: want true, got false")
	}
}

func TestFactZeroValues(t *testing.T) {
	var fact Fact

	if fact.Subject != "" {
		t.Fatalf("zero Subject: want \"\", got %q", fact.Subject)
	}
	if fact.Link != "" {
		t.Fatalf("zero Link: want \"\", got %q", fact.Link)
	}
	if fact.Object != "" {
		t.Fatalf("zero Object: want \"\", got %q", fact.Object)
	}
	if fact.Phase != 0 {
		t.Fatalf("zero Phase: want 0, got %v", fact.Phase)
	}
	if fact.Temporal != 0 {
		t.Fatalf("zero Temporal: want 0, got %v", fact.Temporal)
	}
	if fact.Negated {
		t.Fatalf("zero Negated: want false, got true")
	}
}


