package primitive

import (
	"testing"
)

/*
TestRegionMixing pool confirms that treating Regions as a bounded Queue
allows for writing and reading of independent values.
*/
func TestRegionMixing(t *testing.T) {
	region, err := NewRegion(1)
	if err != nil {
		t.Fatalf("failed to create node A: %v", err)
	}

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
