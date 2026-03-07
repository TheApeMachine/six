package projector

import (
	"regexp"
	"testing"
)

func TestSanitizeLabel(t *testing.T) {
	prompt := "Test Prompt 123!@#"
	label := sanitizeLabel(prompt)

	matched, _ := regexp.MatchString("^[a-z0-9_]+$", label)
	if !matched {
		t.Fatalf("Label %q does not match expected format /^[a-z0-9_]+$/", label)
	}
}
