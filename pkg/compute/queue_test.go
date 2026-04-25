package compute

import (
	"context"
	"testing"
)

func TestQueueNextReturnsScheduledWork(t *testing.T) {
	queue, err := NewQueue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()

	ran := false
	if !queue.Schedule(context.Background(), func() { ran = true }) {
		t.Fatal("schedule failed")
	}

	queue.Next()()

	if !ran {
		t.Fatal("scheduled task did not run")
	}
}

func TestQueueNextPrefersPriorityWork(t *testing.T) {
	queue, err := NewQueue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()

	var order []string
	if !queue.Schedule(context.Background(), func() { order = append(order, "normal") }) {
		t.Fatal("schedule normal failed")
	}
	if !queue.SchedulePriority(context.Background(), func() { order = append(order, "priority") }) {
		t.Fatal("schedule priority failed")
	}

	queue.Next()()
	queue.Next()()

	if got := order[0]; got != "priority" {
		t.Fatalf("first task = %q, want priority", got)
	}
	if got := order[1]; got != "normal" {
		t.Fatalf("second task = %q, want normal", got)
	}
}
