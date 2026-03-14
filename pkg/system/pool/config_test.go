package pool

import (
	"testing"
	"time"
)

func TestNewConfigDefaults(t *testing.T) {
	cfg := NewConfig()
	if cfg == nil {
		t.Fatalf("NewConfig() returned nil")
	}
	expected := 10 * time.Second
	if cfg.SchedulingTimeout != expected {
		t.Fatalf("expected SchedulingTimeout %v, got %v", expected, cfg.SchedulingTimeout)
	}
}
