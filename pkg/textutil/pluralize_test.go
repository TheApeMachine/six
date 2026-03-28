package textutil

import "testing"

func TestPluralize(t *testing.T) {
	if got := Pluralize(1, "cat", "cats"); got != "cat" {
		t.Errorf("Pluralize(1): got %q want %q", got, "cat")
	}
	if got := Pluralize(0, "cat", "cats"); got != "cats" {
		t.Errorf("Pluralize(0): got %q want %q", got, "cats")
	}
	if got := Pluralize(2, "device", "devices"); got != "devices" {
		t.Errorf("Pluralize(2): got %q want %q", got, "devices")
	}
}
