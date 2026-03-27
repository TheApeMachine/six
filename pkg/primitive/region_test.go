package primitive

import (
	"testing"
)

/*
TestRegionMixing pool confirms that treating Regions as a bounded Queue
allows for writing and reading of independent values.
*/
func TestRegionMixing(t *testing.T) {
	region := NewRegion(1)
	defer region.Close()

	// Write 5 Values to the pool
	for i := 0; i < 5; i++ {
		originBuf := make([]byte, ByteSize)
		// set a marker byte so we can verify output
		originBuf[0] = byte(i + 1)
		if _, err := region.Write(originBuf); err != nil {
			t.Fatalf("region write failed: %v", err)
		}
	}

	// Read 5 Values from the pool
	for i := 0; i < 5; i++ {
		readBuf := make([]byte, ByteSize)
		if _, err := region.Read(readBuf); err != nil {
			t.Fatalf("region read failed: %v", err)
		}

		if readBuf[0] != byte(i+1) {
			t.Errorf("expected mixing value %d, got %d", i+1, readBuf[0])
		}
	}
}

/*
TestRegionSpillOverflow verifies Values push to the spill deque when the
buffered channel is saturated, rather than returning ErrShortWrite immediately.
*/
func TestRegionSpillOverflow(t *testing.T) {
	region := NewRegion(1)
	defer region.Close()

	capacity := 64
	for i := 0; i < capacity; i++ {
		buf := make([]byte, ByteSize)
		buf[0] = 1
		if _, err := region.Write(buf); err != nil {
			t.Fatalf("fill channel: %v", err)
		}
	}

	spilled := make([]byte, ByteSize)
	spilled[0] = 42
	if _, err := region.Write(spilled); err != nil {
		t.Fatalf("spill enqueue: want nil err, got %v", err)
	}
	q, acc, drop := region.SpillStats()
	if q != 1 {
		t.Fatalf("spill queued: want 1, got %d", q)
	}
	if acc != 1 || drop != 0 {
		t.Fatalf("stats: want acc=1 drop=0, got acc=%d drop=%d", acc, drop)
	}

	for i := 0; i < capacity; i++ {
		readBuf := make([]byte, ByteSize)
		if _, err := region.Read(readBuf); err != nil {
			t.Fatalf("drain mixer: %v", err)
		}
	}

	readBuf := make([]byte, ByteSize)
	if _, err := region.Read(readBuf); err != nil {
		t.Fatalf("drain spill: %v", err)
	}
	if readBuf[0] != 42 {
		t.Fatalf("spill payload: want marker 42, got %d", readBuf[0])
	}
}
